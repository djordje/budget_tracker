// Package web serves the server-rendered HTML application.
package web

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	site "budgettracker/web"
)

// Handler serves the HTML pages and static assets.
type Handler struct {
	pages  map[string]*template.Template
	static http.Handler
	logger *slog.Logger
	year   int
}

type pageData struct {
	Title string
	Year  int
}

// New parses the embedded templates and prepares the static file handler.
func New(logger *slog.Logger) (*Handler, error) {
	pages, err := parsePages(site.Templates)
	if err != nil {
		return nil, err
	}

	staticRoot, err := fs.Sub(site.Static, "static")
	if err != nil {
		return nil, fmt.Errorf("open static assets: %w", err)
	}

	return &Handler{
		pages:  pages,
		static: http.FileServer(http.FS(staticRoot)),
		logger: logger,
		year:   time.Now().Year(),
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

// Static serves the embedded static assets.
func (h *Handler) Static() http.Handler {
	return h.static
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
