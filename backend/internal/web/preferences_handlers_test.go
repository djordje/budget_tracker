package web

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/google/uuid"

	"budgettracker/internal/auth"
	"budgettracker/internal/households"
)

func TestPreferencesPageUsesPreferenceSpecificErrors(t *testing.T) {
	userID := uuid.New()
	householdID := uuid.New()
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantText   string
	}{
		{name: "household missing", err: households.ErrNotFound, wantStatus: http.StatusNotFound, wantText: "Preferences not found"},
		{name: "forbidden", err: households.ErrForbidden, wantStatus: http.StatusForbidden, wantText: "Not allowed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, err := New(Deps{
				Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
				Sessions: stubSessions{},
				Households: stubHouseholds{
					households:    []households.Household{{ID: householdID, Name: "Home", Role: households.RoleMember}},
					getOptionsErr: tt.err,
				},
				Entries: stubEntries{},
				Rates:   stubRates{},
				Reports: stubReports{},
			})
			require.NoError(t, err, "New()")

			session := auth.Session{
				UserID:            userID,
				User:              auth.User{ID: userID},
				ActiveHouseholdID: &householdID,
			}
			request := httptest.NewRequest(http.MethodGet, "/app/preferences", nil)
			request.AddCookie(&http.Cookie{Name: auth.DefaultSessionCookieName, Value: "raw"})
			recorder := httptest.NewRecorder()
			auth.NewMiddleware(stubSessionLookup{session: session}, auth.CookieConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil))).
				Load(http.HandlerFunc(handler.PreferencesPage)).ServeHTTP(recorder, request)

			require.Equal(t, tt.wantStatus, recorder.Code, "body = %s", recorder.Body.String())
			body := recorder.Body.String()
			assert.Contains(t, body, tt.wantText)
			assert.NotContains(t, body, "Rate not found", "preferences failure used rate-specific copy:\n%s", body)
		})
	}
}
