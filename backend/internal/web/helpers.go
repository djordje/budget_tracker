package web

import (
	"net/http"
	"strings"
	"time"

	"budgettracker/internal/auth"
)

// sessionFromContext returns the session that auth.Middleware.Load attached.
// Every protected route is wrapped in RequireAuth, which already turns away
// anonymous requests, so the zero value is a fail-closed fallback if that
// invariant ever regresses. AcceptInvitation is the one route that checks for
// a session itself because it redirects back to the invitation.
func sessionFromContext(r *http.Request) auth.Session {
	session, _ := auth.FromContext(r.Context())
	return session
}

func parseOptionalDate(raw string) (*time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(entryDateLayout, value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
