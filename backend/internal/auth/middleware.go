package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
)

type sessionContextKey struct{}

type sessionLookup interface {
	SessionByToken(ctx context.Context, rawToken string) (Session, error)
}

// Middleware loads the session referenced by the request cookie.
type Middleware struct {
	sessions sessionLookup
	cookies  CookieConfig
	logger   *slog.Logger
}

// NewMiddleware constructs session-loading middleware.
func NewMiddleware(sessions sessionLookup, cookies CookieConfig, logger *slog.Logger) *Middleware {
	if logger == nil {
		logger = slog.Default()
	}
	return &Middleware{sessions: sessions, cookies: cookies.resolved(), logger: logger}
}

// Load attaches a valid session to the request context. Invalid or expired
// sessions clear the cookie; transient lookup failures leave it intact.
func (m *Middleware) Load(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawToken := m.cookies.Token(r)
		if rawToken == "" {
			next.ServeHTTP(w, r)
			return
		}

		session, err := m.sessions.SessionByToken(r.Context(), rawToken)
		if err != nil {
			if errors.Is(err, ErrSessionNotFound) {
				m.cookies.Clear(w)
			} else {
				m.logger.Warn("load session failed", "error", err)
			}
			next.ServeHTTP(w, r)
			return
		}

		ctx := context.WithValue(r.Context(), sessionContextKey{}, session)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// FromContext returns the authenticated session attached by Middleware.Load.
func FromContext(ctx context.Context) (Session, bool) {
	session, ok := ctx.Value(sessionContextKey{}).(Session)
	return session, ok
}
