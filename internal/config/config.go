package config

import (
	"errors"
	"os"
	"strconv"
	"time"
)

// Config 全部由環境變數載入，沒有設定檔。
type Config struct {
	Port         string
	BackendURL   string
	AdminAPIKey  string
	DBPath       string
	InitAccount  string
	InitPassword string
	CookieSecure bool
	SessionTTL   time.Duration
	AssetDir     string
}

func Load() (Config, error) {
	cfg := Config{
		Port:         getenv("ADMIN_PORT", "8081"),
		BackendURL:   getenv("BACKEND_URL", "http://127.0.0.1:8080"),
		AdminAPIKey:  os.Getenv("ADMIN_API_KEY"),
		DBPath:       getenv("ADMIN_DB_PATH", "./data/admin.db"),
		InitAccount:  getenv("ADMIN_INIT_ACCOUNT", "admin"),
		InitPassword: os.Getenv("ADMIN_INIT_PASSWORD"),
		CookieSecure: getenv("ADMIN_COOKIE_SECURE", "false") == "true",
		AssetDir:     getenv("ADMIN_ASSET_DIR", "./asset"),
	}

	ttlHours, err := strconv.Atoi(getenv("ADMIN_SESSION_HOURS", "12"))
	if err != nil || ttlHours <= 0 {
		return cfg, errors.New("ADMIN_SESSION_HOURS must be a positive integer")
	}
	cfg.SessionTTL = time.Duration(ttlHours) * time.Hour

	if cfg.AdminAPIKey == "" {
		return cfg, errors.New("ADMIN_API_KEY is required (backend rejects admin calls without it)")
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
