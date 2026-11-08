package web

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"budgettracker/internal/auth"
)

type stubSessions struct {
	session auth.Session
	err     error
}

func (s stubSessions) Register(context.Context, auth.RegisterParams) (auth.Session, error) {
	return s.session, s.err
}

func (s stubSessions) Login(context.Context, string, string) (auth.Session, error) {
	return s.session, s.err
}

func (s stubSessions) DeleteSession(context.Context, string) error {
	return s.err
}

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	handler, err := New(Deps{
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Sessions: stubSessions{},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return handler
}

func TestHome(t *testing.T) {
	handler := newTestHandler(t)
	recorder := httptest.NewRecorder()

	handler.Home(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

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
		`href="/register"`,
		`href="/login"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body does not contain %q", want)
		}
	}
}

func TestAuthPagesRenderForms(t *testing.T) {
	handler := newTestHandler(t)

	tests := []struct {
		name   string
		path   string
		handle http.HandlerFunc
		want   []string
	}{
		{
			name:   "login",
			path:   "/login",
			handle: handler.LoginPage,
			want:   []string{"Sign in", `action="/login"`, `name="email"`, `name="password"`, `name="csrf_token"`},
		},
		{
			name:   "register",
			path:   "/register",
			handle: handler.RegisterPage,
			want:   []string{"Create your account", `action="/register"`, `name="first_name"`, `name="last_name"`, `name="password_confirm"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			tt.handle(recorder, httptest.NewRequest(http.MethodGet, tt.path, nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
			}
			body := recorder.Body.String()
			for _, want := range tt.want {
				if !strings.Contains(body, want) {
					t.Errorf("body does not contain %q", want)
				}
			}
		})
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

func TestStaticDoesNotListDirectories(t *testing.T) {
	handler := newTestHandler(t)
	server := httptest.NewServer(http.StripPrefix("/static/", handler.Static()))
	defer server.Close()

	response, err := http.Get(server.URL + "/static/")
	if err != nil {
		t.Fatalf("GET static directory: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", response.StatusCode, http.StatusNotFound)
	}
}

func TestStaticRejectsDotfilesAndDirectories(t *testing.T) {
	staticDir := t.TempDir()
	files := map[string]string{
		".env":                  "secret",
		"nested/.secret":        "secret",
		"nested/file.txt":       "visible",
		"css/app.css":           "body {}",
		".well-known/thing.txt": "hidden",
	}
	for name, contents := range files {
		fullPath := filepath.Join(staticDir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o700); err != nil {
			t.Fatalf("create static fixture directory: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte(contents), 0o600); err != nil {
			t.Fatalf("write static fixture %s: %v", name, err)
		}
	}

	handler, err := New(Deps{
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Sessions:  stubSessions{},
		StaticDir: staticDir,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server := httptest.NewServer(http.StripPrefix("/static/", handler.Static()))
	defer server.Close()

	tests := []struct {
		path       string
		wantStatus int
	}{
		{path: "/static/.env", wantStatus: http.StatusNotFound},
		{path: "/static/nested/.secret", wantStatus: http.StatusNotFound},
		{path: "/static/.well-known/thing.txt", wantStatus: http.StatusNotFound},
		{path: "/static/nested", wantStatus: http.StatusNotFound},
		{path: "/static/nested/file.txt", wantStatus: http.StatusOK},
		{path: "/static/css/app.css", wantStatus: http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			response, err := http.Get(server.URL + tt.path)
			if err != nil {
				t.Fatalf("GET %s: %v", tt.path, err)
			}
			defer response.Body.Close()
			if response.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", response.StatusCode, tt.wantStatus)
			}
		})
	}
}

func TestStaticUsesConfiguredDirectory(t *testing.T) {
	staticDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticDir, "custom.txt"), []byte("from disk"), 0o600); err != nil {
		t.Fatalf("write static fixture: %v", err)
	}

	handler, err := New(Deps{
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Sessions:  stubSessions{},
		StaticDir: staticDir,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server := httptest.NewServer(http.StripPrefix("/static/", handler.Static()))
	defer server.Close()

	response, err := http.Get(server.URL + "/static/custom.txt")
	if err != nil {
		t.Fatalf("GET configured static asset: %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read configured static asset: %v", err)
	}
	if string(body) != "from disk" {
		t.Errorf("body = %q, want from disk", body)
	}
}

func TestNewRejectsMissingStaticDirectory(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	if _, err := New(Deps{
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Sessions:  stubSessions{},
		StaticDir: missing,
	}); err == nil {
		t.Fatal("New() error = nil, want error")
	}
}

func TestNewRequiresSessionService(t *testing.T) {
	if _, err := New(Deps{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}); err == nil {
		t.Fatal("New() error = nil, want error")
	}
}

func TestParsePagesRequiresPages(t *testing.T) {
	if _, err := parsePages(fstest.MapFS{}); err == nil {
		t.Fatal("parsePages() error = nil, want error")
	}
}
