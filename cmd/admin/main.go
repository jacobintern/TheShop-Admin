package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jacobintern/TheShop/admin/internal/backend"
	"github.com/jacobintern/TheShop/admin/internal/config"
	"github.com/jacobintern/TheShop/admin/internal/store"
	"github.com/jacobintern/TheShop/admin/internal/web"
)

func main() {
	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open sqlite %s: %v", cfg.DBPath, err)
	}
	defer st.Close()

	if err := seedAdmin(cfg, st); err != nil {
		log.Fatalf("seed admin: %v", err)
	}

	srv, err := web.New(cfg, st, backend.New(cfg.BackendURL, cfg.AdminAPIKey))
	if err != nil {
		log.Fatalf("init web: %v", err)
	}

	httpSrv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go cleanupSessions(ctx, st)

	go func() {
		log.Printf("admin listening on %s (backend: %s, db: %s)", httpSrv.Addr, cfg.BackendURL, cfg.DBPath)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

// seedAdmin 只在資料庫完全沒有管理員時，用 ADMIN_INIT_ACCOUNT／ADMIN_INIT_PASSWORD 建第一個帳號。
func seedAdmin(cfg config.Config, st *store.Store) error {
	ctx := context.Background()
	n, err := st.CountAccounts(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	if cfg.InitPassword == "" {
		return errors.New("no admin account exists; set ADMIN_INIT_ACCOUNT / ADMIN_INIT_PASSWORD for the first run")
	}
	if _, err := st.CreateAccount(ctx, cfg.InitAccount, cfg.InitPassword); err != nil {
		return err
	}
	log.Printf("created initial admin account %q", cfg.InitAccount)
	return nil
}

func cleanupSessions(ctx context.Context, st *store.Store) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := st.DeleteExpiredSessions(ctx); err != nil {
				log.Printf("cleanup sessions: %v", err)
			}
		}
	}
}
