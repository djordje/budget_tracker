// Package server assembles the HTTP application from its adapters and services.
package server

import (
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"budgettracker/internal/auth"
	"budgettracker/internal/households"
	"budgettracker/internal/httpapi"
	"budgettracker/internal/web"
)

// Deps contains the dependencies needed to build the HTTP handler.
type Deps struct {
	Logger        *slog.Logger
	DB            *pgxpool.Pool
	StaticDir     string
	SecureCookies bool
}

// New constructs the complete HTTP handler.
func New(deps Deps) (http.Handler, error) {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}

	authService := auth.NewService(deps.DB)
	householdService := households.NewService(deps.DB)
	cookies := auth.CookieConfig{Secure: deps.SecureCookies}
	csrf := httpapi.NewCSRF(httpapi.CSRFConfig{Secure: deps.SecureCookies})
	sessions := auth.NewMiddleware(authService, cookies, deps.Logger)

	webHandler, err := web.New(web.Deps{
		Logger:    deps.Logger,
		Sessions:  authService,
		Cookies:   cookies,
		StaticDir: deps.StaticDir,
	})
	if err != nil {
		return nil, err
	}
	health := httpapi.NewHealth(deps.DB, deps.Logger)
	householdAPI := httpapi.NewHouseholdHandler(householdService, deps.Logger)

	protected := http.NewServeMux()
	protected.Handle("GET /{$}", http.HandlerFunc(webHandler.Home))
	protected.Handle("GET /login", http.HandlerFunc(webHandler.LoginPage))
	protected.Handle("POST /login", http.HandlerFunc(webHandler.Login))
	protected.Handle("GET /register", http.HandlerFunc(webHandler.RegisterPage))
	protected.Handle("POST /register", http.HandlerFunc(webHandler.Register))
	protected.Handle("POST /logout", web.RequireAuth(webHandler.Logout))
	protected.Handle("GET /app", web.RequireAuth(webHandler.Dashboard))
	protected.Handle("GET /api/v1/households", httpapi.RequireAuthAPI(householdAPI.List))
	protected.Handle("POST /api/v1/households", httpapi.RequireAuthAPI(householdAPI.Create))
	protected.Handle("GET /api/v1/households/{householdID}", httpapi.RequireAuthAPI(householdAPI.Get))
	protected.Handle("GET /api/v1/households/{householdID}/members", httpapi.RequireAuthAPI(householdAPI.Members))
	protected.Handle("GET /api/v1/households/{householdID}/options", httpapi.RequireAuthAPI(householdAPI.Options))

	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", webHandler.Static()))
	mux.Handle("GET /healthz", http.HandlerFunc(health.Live))
	mux.Handle("GET /readyz", http.HandlerFunc(health.Ready))
	mux.Handle("GET /api/v1/health", http.HandlerFunc(health.Live))
	mux.Handle("/", sessions.Load(csrf.Protect(protected)))

	handler := httpapi.RequestID(
		httpapi.RequestLogger(deps.Logger)(
			httpapi.Recoverer(deps.Logger)(mux),
		),
	)
	return handler, nil
}
