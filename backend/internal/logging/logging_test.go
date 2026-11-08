package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestNewUsesJSONInProduction(t *testing.T) {
	var output bytes.Buffer
	logger := New(&output, "production", slog.LevelInfo)
	logger.Info("started")

	var entry map[string]any
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatalf("log output is not JSON: %v\n%s", err, output.String())
	}
	if entry["service"] != "budget-tracker" {
		t.Errorf("service = %v, want budget-tracker", entry["service"])
	}
	if entry["env"] != "production" {
		t.Errorf("env = %v, want production", entry["env"])
	}
	if entry["msg"] != "started" {
		t.Errorf("msg = %v, want started", entry["msg"])
	}
}

func TestNewUsesTextOutsideProduction(t *testing.T) {
	var output bytes.Buffer
	logger := New(&output, "development", slog.LevelInfo)
	logger.Info("started")

	got := output.String()
	if strings.HasPrefix(strings.TrimSpace(got), "{") {
		t.Fatalf("log output unexpectedly is JSON: %s", got)
	}
	if !strings.Contains(got, "service=budget-tracker") {
		t.Errorf("log output missing service attribute: %s", got)
	}
}

func TestSlogSetDefaultUsesApplicationLogger(t *testing.T) {
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })

	var output bytes.Buffer
	logger := New(&output, "development", slog.LevelInfo)
	slog.SetDefault(logger)

	slog.Info("fallback")
	if !strings.Contains(output.String(), "service=budget-tracker") {
		t.Errorf("fallback log output missing application metadata: %s", output.String())
	}
}

func TestNewHonorsLevel(t *testing.T) {
	var output bytes.Buffer
	logger := New(&output, "development", slog.LevelInfo)
	logger.Debug("hidden")

	if output.Len() != 0 {
		t.Errorf("debug log was written at info level: %s", output.String())
	}
}
