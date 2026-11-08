// Package web serves the server-rendered HTML application.
package web

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	site "budgettracker/web"
)

// Handler serves the HTML pages and static assets.
type Handler struct {
	pages    map[string]*template.Template
	static   http.Handler
	staticFS fs.FS
	logger   *slog.Logger
	year     int
}

type pageData struct {
	Title string
	Year  int
}

// New parses the embedded templates and prepares the static file handler. A
// non-empty staticDir serves assets from disk for local development; otherwise
// the embedded assets are used.
func New(logger *slog.Logger, staticDir string) (*Handler, error) {
	pages, err := parsePages(site.Templates)
	if err != nil {
		return nil, err
	}

	var staticRoot fs.FS
	if staticDir != "" {
		info, err := os.Stat(staticDir)
		if err != nil {
			return nil, fmt.Errorf("open static directory %q: %w", staticDir, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("static path %q is not a directory", staticDir)
		}
		staticRoot = os.DirFS(staticDir)
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
		logger:   logger,
		year:     time.Now().Year(),
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

// Home serves the application landing page.
func (h *Handler) Home(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "home", pageData{
		Title: "Budget Tracker",
		Year:  h.year,
	})
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

func (h *Handler) render(w http.ResponseWriter, r *http.Request, page string, data any) {
	tmpl, ok := h.pages[page]
	if !ok {
		h.logger.Error("page template not found", "page", page)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var body bytes.Buffer
	if err := tmpl.ExecuteTemplate(&body, "layout", data); err != nil {
		h.logger.Error("render page", "page", page, "path", r.URL.Path, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	if _, err := body.WriteTo(w); err != nil {
		h.logger.Error("write page", "page", page, "path", r.URL.Path, "error", err)
	}
}
