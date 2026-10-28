package app

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-sql-driver/mysql"
)

type Config struct{ FrontendDir, AdminUsername, AdminPassword, DingID, DingSecret, DingCorp, DingAPI, DingOAPI, DingTokenURL, DingUserURL, AllowedOrigin string }
type App struct {
	DB         *sql.DB
	Config     Config
	mux        *http.ServeMux
	heavy      chan struct{}
	authWork   chan struct{}
	syncMu     sync.Mutex
	syncing    bool
	client     *http.Client
	tokenMu    sync.Mutex
	token      string
	tokenUntil time.Time
	loginMu    sync.Mutex
	attempts   map[string]attempt
}
type attempt struct {
	count int
	until time.Time
}
type ctxKey int

const actorKey ctxKey = 1

func New(db *sql.DB, cfg Config) *App {
	a := &App{DB: db, Config: cfg, mux: http.NewServeMux(), heavy: make(chan struct{}, 1), authWork: make(chan struct{}, 2), client: &http.Client{Timeout: 15 * time.Second}, attempts: map[string]attempt{}}
	a.routes()
	return a
}
func randomID() string {
	b := make([]byte, 24)
	_, e := rand.Read(b)
	must(e)
	return hex.EncodeToString(b)
}
func actor(r *http.Request) Row { u, _ := r.Context().Value(actorKey).(Row); return u }
func restricted(u Row) bool     { return u != nil && (u["role"] == "employee" || u["role"] == "approver") }
func roles(r *http.Request, allowed ...string) {
	for _, s := range allowed {
		if actor(r)["role"] == s {
			return
		}
	}
	fail(403, "没有权限执行此操作")
}
func body(r *http.Request) Row {
	var b Row
	d := json.NewDecoder(r.Body)
	d.UseNumber()
	if e := d.Decode(&b); e != nil || b == nil {
		fail(400, "请求格式错误")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		fail(400, "请求格式错误")
	}
	return b
}
func send(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		must(json.NewEncoder(w).Encode(v))
	}
}
func routeID(r *http.Request, key string) int64 {
	return integer(r.PathValue(key), 0, 9007199254740991)
}
func page(r *http.Request, key string, def, max int64) int64 {
	s := r.URL.Query().Get(key)
	if s == "" {
		return def
	}
	return integer(s, 0, max)
}
func (a *App) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if p := recover(); p != nil {
				switch v := p.(type) {
				case problem:
					send(w, v.Status, Row{"message": v.Message})
				case *mysql.MySQLError:
					if v.Number == 1062 {
						send(w, 409, Row{"message": "记录已存在或请求已处理，请刷新后重试"})
					} else if v.Number == 1213 || v.Number == 1205 {
						send(w, 409, Row{"message": "数据正在更新，请刷新后重试"})
					} else {
						slog.Error("database request failed", "code", v.Number)
						send(w, 500, Row{"message": "操作失败，本次操作已回滚"})
					}
				default:
					slog.Error("request failed", "error", fmt.Sprint(p))
					send(w, 500, Row{"message": "操作失败，本次操作已回滚"})
				}
			}
		}()
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		if a.Config.AllowedOrigin != "" && r.Header.Get("Origin") == a.Config.AllowedOrigin {
			w.Header().Set("Access-Control-Allow-Origin", a.Config.AllowedOrigin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, OPTIONS")
			w.Header().Set("Access-Control-Expose-Headers", "Content-Disposition")
		}
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 21<<20)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
			public := r.Method == "GET" && (r.URL.Path == "/api/health" || r.URL.Path == "/api/auth/dingtalk/config") || r.Method == "POST" && (r.URL.Path == "/api/auth/login" || r.URL.Path == "/api/auth/dingtalk/login")
			if !public {
				u := a.resolve(r.Context(), bearer(r))
				if u == nil {
					fail(401, "请先登录")
				}
				r = r.WithContext(context.WithValue(r.Context(), actorKey, u))
			}
		}
		a.mux.ServeHTTP(w, r)
	})
}
func bearer(r *http.Request) string {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return parts[1]
	}
	return ""
}
func (a *App) heavyWork(fn func()) {
	select {
	case a.heavy <- struct{}{}:
		defer func() { <-a.heavy }()
		fn()
	default:
		fail(429, "导入导出任务正在执行，请稍后重试")
	}
}
func (a *App) routes() {
	m := a.mux
	m.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if a.DB.PingContext(ctx) != nil {
			fail(503, "数据库暂不可用")
		}
		send(w, 200, Row{"ok": true})
	})
	m.HandleFunc("POST /api/auth/login", a.login)
	m.HandleFunc("GET /api/auth/me", func(w http.ResponseWriter, r *http.Request) { send(w, 200, Row{"user": actor(r)}) })
	m.HandleFunc("POST /api/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		exec(r.Context(), a.DB, "DELETE FROM sessions WHERE token_hash = ?", hash(bearer(r)))
		w.WriteHeader(204)
	})
	m.HandleFunc("GET /api/auth/dingtalk/config", func(w http.ResponseWriter, r *http.Request) {
		send(w, 200, Row{"enabled": a.dingConfigured(), "corpId": a.Config.DingCorp})
	})
	m.HandleFunc("POST /api/auth/dingtalk/login", a.dingLogin)
	m.HandleFunc("GET /api/auth/users", a.users)
	m.HandleFunc("POST /api/auth/users", a.createUser)
	m.HandleFunc("PATCH /api/auth/users/{id}", a.updateUser)
	m.HandleFunc("GET /api/assets", a.listAssets)
	m.HandleFunc("GET /api/assets/options", a.assetOptions)
	m.HandleFunc("GET /api/assets/{code}", a.getAsset)
	m.HandleFunc("PATCH /api/assets/{code}", a.updateAsset)
	m.HandleFunc("POST /api/assets/{code}/qr", a.saveQR)
	m.HandleFunc("POST /api/assets/{code}/events", func(w http.ResponseWriter, r *http.Request) {
		roles(r, "admin", "asset_manager")
		fail(409, "资产流转需先提交审批申请，全部通过后更新台账")
	})
	m.HandleFunc("GET /api/stats", a.stats)
	m.HandleFunc("GET /api/flows/options", a.flowOptions)
	m.HandleFunc("GET /api/flows", a.listFlows)
	m.HandleFunc("POST /api/flows", a.createFlow)
	m.HandleFunc("GET /api/flows/{id}", a.getFlow)
	m.HandleFunc("POST /api/flows/{id}/{action}", a.flowAction)
	m.HandleFunc("GET /api/inventory/tasks", a.inventoryTasks)
	m.HandleFunc("POST /api/inventory/tasks", a.createInventory)
	m.HandleFunc("GET /api/inventory/tasks/{id}", a.getInventory)
	m.HandleFunc("POST /api/inventory/tasks/{id}/records", a.scanInventory)
	m.HandleFunc("POST /api/inventory/tasks/{id}/complete", a.completeInventory)
	m.HandleFunc("POST /api/imports/preview", a.previewImport)
	m.HandleFunc("POST /api/imports/confirm", a.confirmImport)
	m.HandleFunc("GET /api/exports/{kind}", a.export)
	m.HandleFunc("GET /api/exports/inventory/{id}", a.export)
	m.HandleFunc("GET /api/directory/employees", a.employees)
	m.HandleFunc("GET /api/companies", a.companies)
	m.HandleFunc("POST /api/companies", a.saveCompany)
	m.HandleFunc("PATCH /api/companies/{id}", a.saveCompany)
	m.HandleFunc("GET /api/directory/status", a.directoryStatus)
	m.HandleFunc("PUT /api/directory/settings", a.directorySettings)
	m.HandleFunc("POST /api/directory/sync", a.directorySync)
	m.HandleFunc("POST /api/directory/sync/start", a.startDirectorySync)
	m.HandleFunc("PATCH /api/directory/employees/{id}", a.employeeStatus)
	m.HandleFunc("GET /api/directory/mappings", a.mappings)
	m.HandleFunc("POST /api/directory/mappings/{code}", a.bindEmployee)
	m.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { send(w, 404, Row{"message": "接口不存在"}) })
	if a.Config.FrontendDir != "" {
		m.Handle("/", http.FileServer(http.Dir(a.Config.FrontendDir)))
	} else {
		m.HandleFunc("/", http.NotFound)
	}
}
func (a *App) Scheduler(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	last := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			func() {
				defer func() {
					if p := recover(); p != nil {
						slog.Error("directory scheduler failed", "error", fmt.Sprint(p))
					}
				}()
				cfg := a.settings(ctx)
				exec(ctx, a.DB, "DELETE FROM sessions WHERE expires_at<? LIMIT 500", time.Now().UnixMilli())
				exec(ctx, a.DB, "DELETE FROM import_previews WHERE expires_at<? LIMIT 100", time.Now().Add(-24*time.Hour).UnixMilli())
				interval := num(cfg["intervalMinutes"])
				if a.dingConfigured() && interval > 0 && time.Since(last) >= time.Duration(interval)*time.Minute {
					last = time.Now()
					a.syncDirectory(ctx, nil)
				}
			}()
		}
	}
}
func EnvInt(name string, def int) int {
	if n, e := strconv.Atoi(os.Getenv(name)); e == nil && n > 0 {
		return n
	}
	return def
}
