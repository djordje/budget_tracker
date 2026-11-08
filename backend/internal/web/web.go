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

	"github.com/google/uuid"

	"budgettracker/internal/auth"
	"budgettracker/internal/finance"
	"budgettracker/internal/households"
	site "budgettracker/web"
)

// SessionService is the authentication behavior needed by HTML handlers.
type SessionService interface {
	Register(ctx context.Context, params auth.RegisterParams) (auth.Session, error)
	Login(ctx context.Context, email, password string) (auth.Session, error)
	DeleteSession(ctx context.Context, rawToken string) error
	SetActiveHousehold(ctx context.Context, tokenHash string, userID uuid.UUID, householdID *uuid.UUID) error
}

// EntryService is the finance behavior needed by HTML handlers.
type EntryService interface {
	Currencies(ctx context.Context) ([]finance.Currency, error)
	Create(ctx context.Context, params finance.CreateParams) (finance.Entry, error)
	Get(ctx context.Context, actorID uuid.UUID, kind finance.Kind, id int64) (finance.Entry, error)
	Update(ctx context.Context, params finance.UpdateParams) (finance.Entry, error)
	Delete(ctx context.Context, actorID uuid.UUID, kind finance.Kind, id int64) error
	List(ctx context.Context, params finance.ListParams) (finance.ListResult, error)
}

// HouseholdService is the household behavior needed by HTML handlers.
type HouseholdService interface {
	CreateAndActivate(ctx context.Context, userID uuid.UUID, sessionTokenHash, name string) (households.Household, error)
	List(ctx context.Context, userID uuid.UUID) ([]households.Household, error)
	Rename(ctx context.Context, userID, householdID uuid.UUID, name string) (households.Household, error)
	Members(ctx context.Context, userID, householdID uuid.UUID) ([]households.Member, error)
	GetOptions(ctx context.Context, userID, householdID uuid.UUID) (households.Options, error)
}

// Deps contains the dependencies needed by the HTML application.
type Deps struct {
	Logger     *slog.Logger
	Sessions   SessionService
	Households HouseholdService
	Entries    EntryService
	Cookies    auth.CookieConfig
	StaticDir  string
}

// Handler serves the HTML pages and static assets.
type Handler struct {
	pages      map[string]*template.Template
	static     http.Handler
	staticFS   fs.FS
	logger     *slog.Logger
	sessions   SessionService
	households HouseholdService
	entries    EntryService
	cookies    auth.CookieConfig
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
	if deps.Households == nil {
		return nil, fmt.Errorf("household service is required")
	}
	if deps.Entries == nil {
		return nil, fmt.Errorf("entry service is required")
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
		pages:      pages,
		static:     http.FileServer(http.FS(staticRoot)),
		staticFS:   staticRoot,
		logger:     deps.Logger,
		sessions:   deps.Sessions,
		households: deps.Households,
		entries:    deps.Entries,
		cookies:    deps.Cookies,
	}, nil
}

func parsePages(fsys fs.FS) (map[string]*template.Template, error) {
	files, err := fs.Glob(fsys, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("list templates: %w", err)
	}

	partials := make([]string, 0, len(files))
	for _, file := range files {
		if strings.HasPrefix(path.Base(file), "_") {
			partials = append(partials, file)
		}
	}

	pages := make(map[string]*template.Template, len(files))
	for _, file := range files {
		base := path.Base(file)
		if base == "layout.html" || strings.HasPrefix(base, "_") {
			continue
		}
		name := strings.TrimSuffix(base, ".html")

		patterns := append([]string{"templates/layout.html"}, partials...)
		patterns = append(patterns, file)
		page, err := template.ParseFS(fsys, patterns...)
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
