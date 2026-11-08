// Package logging configures structured application logs.
package logging

import (
	"io"
	"log/slog"

	"budgettracker/internal/version"
)

// New returns a logger with a handler suited to the environment.
func New(output io.Writer, env string, level slog.Level) *slog.Logger {
	options := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if env == "production" {
		handler = slog.NewJSONHandler(output, options)
	} else {
		handler = slog.NewTextHandler(output, options)
	}

	return slog.New(handler).With(
		"service", "budget-tracker",
		"env", env,
		"version", version.Version,
	)
}
