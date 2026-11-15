package web

import (
	"net/http"

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
