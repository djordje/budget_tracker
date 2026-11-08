// Package web serves the server-rendered HTML application.
package web

import (
	"context"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"budgettracker/internal/auth"
	site "budgettracker/web"
)

// SessionService is the authentication behavior needed by HTML handlers.
type SessionService interface {
	Register(ctx context.Context, params auth.RegisterParams) (auth.Session, error)
	Login(ctx context.Context, email, password string) (auth.Session, error)
	DeleteSession(ctx context.Context, rawToken string) error
}

// Deps contains the dependencies needed by the HTML application.
type Deps struct {
	Logger    *slog.Logger
	Sessions  SessionService
	Cookies   auth.CookieConfig
	StaticDir string
}

// Handler serves the HTML pages and static assets.
type Handler struct {
	pages    map[string]*template.Template
	static   http.Handler
	staticFS fs.FS
	logger   *slog.Logger
	year     int
	sessions SessionService
	cookies  auth.CookieConfig
}

// New parses the embedded templates and prepares the static file handler. A
// non-empty staticDir serves assets from disk for local development; otherwise
// the embedded assets are used.
func New(deps Deps) (*Handler, error) {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if deps.Sessions == nil {
		return nil, fmt.Errorf("session service is required")
	}

	pages, err := parsePages(site.Templates)
	if err != nil {
		return nil, err
	}

	var staticRoot fs.FS
	if deps.StaticDir != "" {
		info, err := os.Stat(deps.StaticDir)
		if err != nil {
			return nil, fmt.Errorf("open static directory %q: %w", deps.StaticDir, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("static path %q is not a directory", deps.StaticDir)
		}
		staticRoot = os.DirFS(deps.StaticDir)
	} else {
		staticRoot, err = fs.Sub(site.Static, "static")
		if err != nil {
			return nil, fmt.Errorf("open static assets: %w", err)
		}
	}

	return &Handler{
		pages:    pages,
		static:   http.FileServer(http.FS(staticRoot)),
		staticFS: staticRoot,
		logger:   deps.Logger,
		year:     time.Now().Year(),
		sessions: deps.Sessions,
		cookies:  deps.Cookies,
	}, nil
}

func parsePages(fsys fs.FS) (map[string]*template.Template, error) {
	files, err := fs.Glob(fsys, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("list templates: %w", err)
	}

	pages := make(map[string]*template.Template, len(files))
	for _, file := range files {
		name := strings.TrimSuffix(path.Base(file), ".html")
		if name == "layout" {
			continue
		}

		page, err := template.ParseFS(fsys, "templates/layout.html", file)
		if err != nil {
			return nil, fmt.Errorf("parse template %q: %w", file, err)
		}
		pages[name] = page
	}

	if len(pages) == 0 {
		return nil, fmt.Errorf("no page templates found")
	}

	return pages, nil
}

// Static serves static assets. Directory listings, trailing-slash paths, and
// any path segment beginning with a dot are not exposed, including root-level
// and nested dotfiles.
func (h *Handler) Static() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assetPath := strings.TrimPrefix(r.URL.Path, "/")
		if assetPath == "" || strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		for _, segment := range strings.Split(assetPath, "/") {
			if segment == "" || strings.HasPrefix(segment, ".") {
				http.NotFound(w, r)
				return
			}
		}
		info, err := fs.Stat(h.staticFS, assetPath)
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		h.static.ServeHTTP(w, r)
	})
}
