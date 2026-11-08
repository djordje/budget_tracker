package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"budgettracker/internal/config"
	"budgettracker/internal/httpapi"
	"budgettracker/internal/logging"
	"budgettracker/internal/store"
	"budgettracker/internal/web"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	logger := logging.New(os.Stdout, cfg.Env, cfg.LogLevel)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer db.Close()

	health := httpapi.NewHealth(db, logger)

	webHandler, err := web.New(logger, cfg.StaticDir)
	if err != nil {
		return fmt.Errorf("create web handler: %w", err)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /{$}", http.RedirectHandler("/app", http.StatusPermanentRedirect))
	mux.Handle("GET /app", http.HandlerFunc(webHandler.Home))
	mux.Handle("GET /static/", http.StripPrefix("/static/", webHandler.Static()))
	mux.Handle("GET /healthz", http.HandlerFunc(health.Live))
	mux.Handle("GET /readyz", http.HandlerFunc(health.Ready))
	mux.Handle("GET /api/v1/health", http.HandlerFunc(health.Live))

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.RequestID(httpapi.RequestLogger(logger)(httpapi.Recoverer(logger)(mux))),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", cfg.HTTPAddr)
		errCh <- server.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", err)
		}
		return nil
	case <-ctx.Done():
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shut down HTTP server: %w", err)
		}
		return nil
	}
}
