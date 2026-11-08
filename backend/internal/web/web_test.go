package web

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	handler, err := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return handler
}

func TestHome(t *testing.T) {
	handler := newTestHandler(t)
	recorder := httptest.NewRecorder()

	handler.Home(recorder, httptest.NewRequest(http.MethodGet, "/app", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}

	body := recorder.Body.String()
	for _, want := range []string{
		`<html lang="en"`,
		"<title>Budget Tracker</title>",
		"A clear picture of your household",
		`href="/static/css/app.css"`,
		`href="/static/favicon.svg"`,
		"Skip to content",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body does not contain %q", want)
		}
	}
}

func TestStatic(t *testing.T) {
	handler := newTestHandler(t)
	server := httptest.NewServer(http.StripPrefix("/static/", handler.Static()))
	defer server.Close()

	response, err := http.Get(server.URL + "/static/favicon.svg")
	if err != nil {
		t.Fatalf("GET static asset: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if got := response.Header.Get("Content-Type"); !strings.Contains(got, "image/svg+xml") {
		t.Errorf("Content-Type = %q, want image/svg+xml", got)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read static asset: %v", err)
	}
	if !strings.Contains(string(body), "<svg") {
		t.Error("static asset does not contain SVG markup")
	}
}

func TestParsePagesRequiresPages(t *testing.T) {
	if _, err := parsePages(fstest.MapFS{}); err == nil {
		t.Fatal("parsePages() error = nil, want error")
	}
}
