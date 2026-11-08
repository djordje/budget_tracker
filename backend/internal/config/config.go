package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

const defaultHTTPAddr = ":8080"

// Config contains the runtime settings loaded from the environment.
type Config struct {
	Env         string
	HTTPAddr    string
	DatabaseURL string
	LogLevel    slog.Level
	StaticDir   string
}

// Load reads configuration from the environment. DATABASE_URL is required in
// every environment; APP_ENV accepts only development or production.
func Load() (Config, error) {
	env := valueOrDefault("APP_ENV", "development")
	switch env {
	case "development", "production":
	default:
		return Config{}, fmt.Errorf("APP_ENV must be development or production, got %q", env)
	}

	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL must be set")
	}

	var level slog.Level
	rawLevel := valueOrDefault("LOG_LEVEL", "info")
	if err := level.UnmarshalText([]byte(rawLevel)); err != nil {
		return Config{}, fmt.Errorf("invalid LOG_LEVEL %q: %w", rawLevel, err)
	}

	return Config{
		Env:         env,
		HTTPAddr:    valueOrDefault("HTTP_ADDR", defaultHTTPAddr),
		DatabaseURL: databaseURL,
		LogLevel:    level,
		StaticDir:   strings.TrimSpace(os.Getenv("WEB_STATIC_DIR")),
	}, nil
}

func valueOrDefault(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}
