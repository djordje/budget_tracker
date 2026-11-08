package config

import (
	"log/slog"
	"testing"
)

func clearEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{"APP_ENV", "HTTP_ADDR", "DATABASE_URL", "LOG_LEVEL", "WEB_STATIC_DIR"} {
		t.Setenv(name, "")
	}
}

func TestLoadDefaults(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("DATABASE_URL", "postgres://localhost/example")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Env != "development" {
		t.Errorf("Env = %q, want development", cfg.Env)
	}
	if cfg.HTTPAddr != defaultHTTPAddr {
		t.Errorf("HTTPAddr = %q, want %q", cfg.HTTPAddr, defaultHTTPAddr)
	}
	if cfg.DatabaseURL != "postgres://localhost/example" {
		t.Errorf("DatabaseURL = %q, want postgres://localhost/example", cfg.DatabaseURL)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want %v", cfg.LogLevel, slog.LevelInfo)
	}
	if cfg.StaticDir != "" {
		t.Errorf("StaticDir = %q, want empty", cfg.StaticDir)
	}
}

func TestLoadOverrides(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("HTTP_ADDR", ":9090")
	t.Setenv("DATABASE_URL", "postgres://example/db")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("WEB_STATIC_DIR", "web/static")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Env != "production" {
		t.Errorf("Env = %q, want production", cfg.Env)
	}
	if cfg.HTTPAddr != ":9090" {
		t.Errorf("HTTPAddr = %q, want :9090", cfg.HTTPAddr)
	}
	if cfg.DatabaseURL != "postgres://example/db" {
		t.Errorf("DatabaseURL = %q, want postgres://example/db", cfg.DatabaseURL)
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v, want %v", cfg.LogLevel, slog.LevelDebug)
	}
	if cfg.StaticDir != "web/static" {
		t.Errorf("StaticDir = %q, want web/static", cfg.StaticDir)
	}
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	clearEnvironment(t)

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want error")
	}
}

func TestLoadRejectsInvalidEnvironment(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("APP_ENV", "staging")
	t.Setenv("DATABASE_URL", "postgres://example/db")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want error")
	}
}

func TestLoadRejectsInvalidLogLevel(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("DATABASE_URL", "postgres://example/db")
	t.Setenv("LOG_LEVEL", "loud")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want error")
	}
}
