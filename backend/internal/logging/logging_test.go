package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewUsesJSONInProduction(t *testing.T) {
	var output bytes.Buffer
	logger := New(&output, "production", slog.LevelInfo)
	logger.Info("started")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(output.Bytes(), &entry), "log output: %s", output.String())

	assert.Equal(t, "budget-tracker", entry["service"])
	assert.Equal(t, "production", entry["env"])
	assert.Equal(t, "started", entry["msg"])
}

func TestNewUsesTextOutsideProduction(t *testing.T) {
	var output bytes.Buffer
	logger := New(&output, "development", slog.LevelInfo)
	logger.Info("started")

	got := output.String()
	assert.False(t, strings.HasPrefix(strings.TrimSpace(got), "{"), "log output is JSON: %s", got)
	assert.Contains(t, got, "service=budget-tracker")
}

func TestSlogSetDefaultUsesApplicationLogger(t *testing.T) {
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })

	var output bytes.Buffer
	logger := New(&output, "development", slog.LevelInfo)
	slog.SetDefault(logger)

	slog.Info("fallback")
	assert.Contains(t, output.String(), "service=budget-tracker")
}

func TestNewHonorsLevel(t *testing.T) {
	var output bytes.Buffer
	logger := New(&output, "development", slog.LevelInfo)
	logger.Debug("hidden")

	assert.Zero(t, output.Len(), "debug log output = %q", output.String())
}
