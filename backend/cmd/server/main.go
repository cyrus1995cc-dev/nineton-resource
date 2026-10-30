package main

import (
	"asset-system/backend/internal/app"
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"
)

func main() {
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(384 << 20)
	}
	defer func() {
		if p := recover(); p != nil {
			slog.Error("startup failed", "error", p)
			os.Exit(1)
		}
	}()
	dsn := os.Getenv("MYSQL_DSN")
	if dsn == "" {
		slog.Error("MYSQL_DSN is required")
		os.Exit(1)
	}
	db, e := app.Open(dsn, app.EnvInt("DB_MAX_OPEN", 8))
	if e != nil {
		slog.Error("database connection failed", "error", e)
		os.Exit(1)
	}
	defer db.Close()
	cfg := app.Config{FrontendDir: os.Getenv("FRONTEND_DIR"), AdminUsername: os.Getenv("ASSET_ADMIN_USERNAME"), AdminPassword: os.Getenv("ASSET_ADMIN_PASSWORD"), DingID: os.Getenv("DINGTALK_CLIENT_ID"), DingSecret: os.Getenv("DINGTALK_CLIENT_SECRET"), DingCorp: os.Getenv("DINGTALK_CORP_ID"), AllowedOrigin: os.Getenv("ALLOWED_ORIGIN")}
	cfg.SSOIssuer = os.Getenv("SSO_ISSUER")
	cfg.SSOClientID = os.Getenv("SSO_CLIENT_ID")
	cfg.SSOClientSecret = os.Getenv("SSO_CLIENT_SECRET")
	cfg.SSORedirectURI = os.Getenv("SSO_REDIRECT_URI")
	cfg.SSOScopes = os.Getenv("SSO_SCOPES")
	cfg.SSOProviderHint = os.Getenv("SSO_PROVIDER_HINT")
	cfg.SSOSubjectIsDingTalkUserID = os.Getenv("SSO_SUBJECT_IS_DINGTALK_USER_ID") == "1"
	a := app.New(db, cfg)
	if os.Getenv("MIGRATE_ON_START") == "1" {
		if e = app.Migrate(context.Background(), db); e != nil {
			slog.Error("schema migration failed", "error", e)
			os.Exit(1)
		}
	}
	a.Bootstrap(context.Background())
	a.ReconcileSSODirectory(context.Background())
	a.RecoverDirectorySync(context.Background())
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go a.Scheduler(ctx)
	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = "127.0.0.1:3000"
	}
	srv := &http.Server{Addr: addr, Handler: a.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 45 * time.Second, WriteTimeout: 120 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	go func() {
		<-ctx.Done()
		shutdown, done := context.WithTimeout(context.Background(), 15*time.Second)
		defer done()
		_ = srv.Shutdown(shutdown)
	}()
	slog.Info("asset API ready", "address", addr)
	if e = srv.ListenAndServe(); e != nil && e != http.ErrServerClosed {
		slog.Error("server failed", "error", e)
		os.Exit(1)
	}
}
