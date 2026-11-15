package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type stubPinger struct {
	err error
}

func (p stubPinger) Ping(context.Context) error {
	return p.err
}

func TestHealthLive(t *testing.T) {
	handler := NewHealth(stubPinger{}, discardLogger())
	recorder := httptest.NewRecorder()

	handler.Live(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	require.Equal(t, http.StatusOK, recorder.Code, "body = %s", recorder.Body.String())
	assert.Equal(t, "application/json; charset=utf-8", recorder.Header().Get("Content-Type"), "Content-Type")

	var body healthResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body), "decode response")
	assert.Equal(t, "ok", body.Status, "status")
}

func TestHealthReady(t *testing.T) {
	tests := []struct {
		name       string
		pingErr    error
		wantStatus int
		wantBody   string
	}{
		{
			name:       "database available",
			wantStatus: http.StatusOK,
			wantBody:   `"status":"ok"`,
		},
		{
			name:       "database unavailable",
			pingErr:    errors.New("connection refused"),
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   `"database":"unavailable"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewHealth(stubPinger{err: tt.pingErr}, discardLogger())
			recorder := httptest.NewRecorder()

			handler.Ready(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))

			require.Equal(t, tt.wantStatus, recorder.Code, "body = %s", recorder.Body.String())
			assert.Contains(t, recorder.Body.String(), tt.wantBody)
		})
	}
}

func TestHealthUsesDefaultLoggerWhenNil(t *testing.T) {
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })

	var output strings.Builder
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))

	handler := NewHealth(stubPinger{err: errors.New("connection refused")}, nil)
	recorder := httptest.NewRecorder()
	handler.Ready(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	assert.Contains(t, output.String(), "readiness check failed", "default logger did not receive the readiness warning")
}
