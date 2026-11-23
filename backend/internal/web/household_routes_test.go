package web

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"budgettracker/internal/auth"
	"budgettracker/internal/households"
)

func TestHouseholdLanding(t *testing.T) {
	a, b, stale := uuid.New(), uuid.New(), uuid.New()
	memberships := []households.Household{{ID: a}, {ID: b}}
	for _, tc := range []struct {
		name string
		list []households.Household
		pref *uuid.UUID
		want string
	}{
		{"no memberships", nil, nil, "/app/households/new"},
		{"no preference", memberships, nil, householdURL(a, "")},
		{"stale preference", memberships, &stale, householdURL(a, "")},
		{"accessible preference", memberships, &b, householdURL(b, "")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHandler(t)
			h.households = stubHouseholds{households: tc.list}
			rec := httptest.NewRecorder()
			h.App(rec, householdRequest(h, http.MethodGet, "/app", tc.pref))
			require.Equal(t, http.StatusSeeOther, rec.Code)
			assert.Equal(t, tc.want, rec.Header().Get("Location"))
		})
	}
}

func TestHouseholdScope(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	h := newTestHandler(t)
	h.households = stubHouseholds{households: []households.Household{{ID: a, Name: "First"}, {ID: b, Name: "Second"}}}
	h.sessions = stubSessions{err: errors.New("preference store unavailable")}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /app/{householdID}", h.Scoped("entries", "", h.HouseholdHome))
	mux.HandleFunc("GET /app/{householdID}/entries", h.Scoped("entries", "entries", h.Entries))
	mux.HandleFunc("POST /app/{householdID}/spendings", h.Scoped("entries", "", h.CreateSpending))
	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, householdURL(a, ""), http.StatusSeeOther},
		{http.MethodGet, householdURL(a, "/entries"), http.StatusOK},
		{http.MethodGet, "/app/not-a-uuid/entries", http.StatusNotFound},
		{http.MethodGet, householdURL(uuid.New(), "/entries"), http.StatusNotFound},
		{http.MethodPost, householdURL(uuid.New(), "/spendings"), http.StatusNotFound},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, householdRequest(h, tc.method, tc.path, &b))
			require.Equal(t, tc.want, rec.Code)
			if tc.want == http.StatusSeeOther {
				assert.Equal(t, householdURL(a, "/entries"), rec.Header().Get("Location"))
			}
			if tc.want == http.StatusOK {
				assert.Contains(t, rec.Body.String(), "First")
			}
		})
	}
}

func householdRequest(h *Handler, method, path string, preferred *uuid.UUID) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	r.AddCookie(&http.Cookie{Name: auth.DefaultSessionCookieName, Value: "raw"})
	session := auth.Session{User: auth.User{ID: uuid.New()}, ActiveHouseholdID: preferred}
	auth.NewMiddleware(stubSessionLookup{session: session}, h.cookies, h.logger).
		Load(http.HandlerFunc(func(_ http.ResponseWriter, loaded *http.Request) { r = loaded })).ServeHTTP(httptest.NewRecorder(), r)
	return r
}
