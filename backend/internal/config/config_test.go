package config

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	require.NoError(t, err, "Load()")

	assert.Equal(t, "development", cfg.Env)
	assert.Equal(t, defaultHTTPAddr, cfg.HTTPAddr)
	assert.Equal(t, "postgres://localhost/example", cfg.DatabaseURL)
	assert.Equal(t, slog.LevelInfo, cfg.LogLevel)
	assert.Empty(t, cfg.StaticDir)
}

func TestLoadOverrides(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("HTTP_ADDR", ":9090")
	t.Setenv("DATABASE_URL", "postgres://example/db")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("WEB_STATIC_DIR", "web/static")

	cfg, err := Load()
	require.NoError(t, err, "Load()")

	assert.Equal(t, "production", cfg.Env)
	assert.Equal(t, ":9090", cfg.HTTPAddr)
	assert.Equal(t, "postgres://example/db", cfg.DatabaseURL)
	assert.Equal(t, slog.LevelDebug, cfg.LogLevel)
	assert.Equal(t, "web/static", cfg.StaticDir)
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	clearEnvironment(t)

	_, err := Load()
	assert.Error(t, err, "Load() with no DATABASE_URL")
}

func TestLoadRejectsInvalidEnvironment(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("APP_ENV", "staging")
	t.Setenv("DATABASE_URL", "postgres://example/db")

	_, err := Load()
	assert.Error(t, err, "Load() with APP_ENV=staging")
}

func TestLoadRejectsInvalidLogLevel(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("DATABASE_URL", "postgres://example/db")
	t.Setenv("LOG_LEVEL", "loud")

	_, err := Load()
	assert.Error(t, err, "Load() with LOG_LEVEL=loud")
}
