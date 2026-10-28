package app

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

//go:embed schema.sql
var schema embed.FS

type Row = map[string]any
type DBTX interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}
type problem struct {
	Status  int
	Message string
}

func fail(code int, message string) { panic(problem{code, message}) }
func must(err error) {
	if err != nil {
		panic(err)
	}
}
func str(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
func num(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	default:
		i, _ := strconv.ParseInt(str(v), 10, 64)
		return i
	}
}
func text(v any, max int, required bool) string {
	s, ok := v.(string)
	if !ok && v != nil {
		fail(400, "请填写有效文本")
	}
	if len([]rune(s)) > max {
		fail(400, fmt.Sprintf("文本最多 %d 字", max))
	}
	s = strings.TrimSpace(s)
	if required && s == "" {
		fail(400, "必填内容不能为空")
	}
	return s
}
func integer(v any, fallback, max int64) int64 {
	if v == nil && fallback > 0 {
		return fallback
	}
	s := str(v)
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 1 || n > max {
		fail(400, "请填写有效的正整数")
	}
	return n
}
func js(v any) string        { b, e := json.Marshal(v); must(e); return string(b) }
func decode(s string, v any) { must(json.Unmarshal([]byte(s), v)) }
func rows(ctx context.Context, q DBTX, query string, args ...any) []Row {
	r, err := q.QueryContext(ctx, query, args...)
	must(err)
	defer r.Close()
	cols, err := r.Columns()
	must(err)
	types, err := r.ColumnTypes()
	must(err)
	out := []Row{}
	for r.Next() {
		values := make([]any, len(cols))
		ptr := make([]any, len(cols))
		for i := range values {
			ptr[i] = &values[i]
		}
		must(r.Scan(ptr...))
		item := Row{}
		for i, c := range cols {
			v := values[i]
			if b, ok := v.([]byte); ok {
				v = string(b)
				switch types[i].DatabaseTypeName() {
				case "DECIMAL", "NEWDECIMAL", "DOUBLE", "FLOAT":
					f, e := strconv.ParseFloat(string(b), 64)
					must(e)
					v = f
				}
			}
			if t, ok := v.(time.Time); ok {
				v = t.UTC().Format("2006-01-02 15:04:05")
			}
			item[c] = v
		}
		out = append(out, item)
	}
	must(r.Err())
	return out
}
func one(ctx context.Context, q DBTX, query string, args ...any) Row {
	r := rows(ctx, q, query, args...)
	if len(r) == 0 {
		return nil
	}
	return r[0]
}
func exec(ctx context.Context, q DBTX, query string, args ...any) sql.Result {
	r, e := q.ExecContext(ctx, query, args...)
	must(e)
	return r
}
func insert(ctx context.Context, q DBTX, query string, args ...any) int64 {
	n, e := exec(ctx, q, query, args...).LastInsertId()
	must(e)
	return n
}
func exists(ctx context.Context, q DBTX, query string, args ...any) bool {
	return one(ctx, q, query, args...) != nil
}
func transaction[T any](ctx context.Context, db *sql.DB, fn func(*sql.Tx) T) T {
	tx, e := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	must(e)
	defer tx.Rollback()
	result := fn(tx)
	must(tx.Commit())
	return result
}

func Open(dsn string, maxOpen int) (*sql.DB, error) {
	cfg, e := mysql.ParseDSN(dsn)
	if e != nil {
		return nil, e
	}
	if cfg.DBName == "" {
		return nil, fmt.Errorf("MYSQL_DSN must select a database")
	}
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.Timeout = 5 * time.Second
	cfg.ReadTimeout = 30 * time.Second
	cfg.WriteTimeout = 30 * time.Second
	cfg.MultiStatements = false
	cfg.InterpolateParams = false
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	cfg.Params["charset"] = "utf8mb4"
	cfg.Params["time_zone"] = "'+00:00'"
	cfg.Collation = "utf8mb4_bin"
	db, e := sql.Open("mysql", cfg.FormatDSN())
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if e = db.PingContext(ctx); e != nil {
		db.Close()
		return nil, e
	}
	return db, nil
}
func Migrate(ctx context.Context, db *sql.DB) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("schema migration: %v", p)
		}
	}()
	b, e := schema.ReadFile("schema.sql")
	if e != nil {
		return e
	}
	for _, stmt := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), ";\n") {
		if strings.TrimSpace(stmt) != "" {
			exec(ctx, db, stmt)
		}
	}
	SeedCompanies(ctx, db)
	return nil
}
func Tables() []string {
	return []string{"employees", "users", "assets", "asset_flows", "asset_events", "import_batches", "inventory_tasks", "inventory_records", "asset_flow_items", "asset_flow_steps", "asset_flow_logs", "asset_flow_comments", "notification_outbox", "asset_qr_aliases", "import_previews", "audit_logs", "departments", "directory_sync_runs", "system_settings", "companies", "employee_departments", "company_names"}
}
