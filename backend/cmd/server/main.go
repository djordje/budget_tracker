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
	"budgettracker/internal/logging"
	"budgettracker/internal/server"
	"budgettracker/internal/store"
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

	handler, err := server.New(server.Deps{
		Logger:        logger,
		DB:            db,
		StaticDir:     cfg.StaticDir,
		SecureCookies: cfg.Env == "production",
	})
	if err != nil {
		return fmt.Errorf("build HTTP handler: %w", err)
	}

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", cfg.HTTPAddr)
		errCh <- httpServer.ListenAndServe()
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

		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shut down HTTP server: %w", err)
		}
		return nil
	}
}
