package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// Pinger checks whether a dependency is reachable.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Health serves liveness and readiness JSON responses.
type Health struct {
	db     Pinger
	logger *slog.Logger
}

// NewHealth constructs the health handlers.
func NewHealth(db Pinger, logger *slog.Logger) *Health {
	if logger == nil {
		logger = slog.Default()
	}
	return &Health{db: db, logger: logger}
}

type healthResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks,omitempty"`
}

// Live reports that the process is running. It does not check dependencies.
func (h *Health) Live(w http.ResponseWriter, _ *http.Request) {
	WriteJSON(w, http.StatusOK, healthResponse{Status: "ok"})
}

// Ready reports whether the database is reachable.
func (h *Health) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := h.db.Ping(ctx); err != nil {
		h.logger.Warn("readiness check failed", "error", err)
		WriteJSON(w, http.StatusServiceUnavailable, healthResponse{
			Status: "unavailable",
			Checks: map[string]string{"database": "unavailable"},
		})
		return
	}

	WriteJSON(w, http.StatusOK, healthResponse{
		Status: "ok",
		Checks: map[string]string{"database": "ok"},
	})
}
