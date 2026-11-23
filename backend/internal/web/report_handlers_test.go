package web

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/google/uuid"

	"budgettracker/internal/auth"
	"budgettracker/internal/finance"
	"budgettracker/internal/households"
)

func TestReportPageUsesReportSpecificErrors(t *testing.T) {
	userID := uuid.New()
	householdID := uuid.New()
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantText   string
	}{
		{name: "household missing", err: households.ErrNotFound, wantStatus: http.StatusNotFound, wantText: "Report not found"},
		{name: "forbidden", err: households.ErrForbidden, wantStatus: http.StatusForbidden, wantText: "Not allowed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, err := New(Deps{
				Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
				Sessions: stubSessions{},
				Households: stubHouseholds{
					households:    []households.Household{{ID: householdID, Name: "Home", Role: households.RoleAdmin}},
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
			request := httptest.NewRequest(http.MethodGet, householdURL(householdID, "/reports"), nil)
			request.SetPathValue("householdID", householdID.String())
			request.AddCookie(&http.Cookie{Name: auth.DefaultSessionCookieName, Value: "raw"})
			recorder := httptest.NewRecorder()
			auth.NewMiddleware(stubSessionLookup{session: session}, auth.CookieConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil))).
				Load(handler.Scoped("reports", "reports", handler.ReportsPage)).ServeHTTP(recorder, request)

			require.Equal(t, tt.wantStatus, recorder.Code, "body = %s", recorder.Body.String())
			body := recorder.Body.String()
			assert.Contains(t, body, tt.wantText)
			assert.NotContains(t, body, "Rate not found", "report failure used rate-specific copy:\n%s", body)
		})
	}
}

func TestReportDefaultsToUTCMonth(t *testing.T) {
	before := finance.Today()
	recorder := serveHouseholdPage(t, func(h *Handler) http.HandlerFunc { return h.ReportsPage })
	// A request can straddle UTC midnight, so accept the dates observed on
	// either side of it instead of racing the clock.
	after := finance.Today()

	require.Equal(t, http.StatusOK, recorder.Code, "body = %s", recorder.Body.String())
	body := recorder.Body.String()
	assert.Contains(t, []string{before.Format(entryDateLayout), after.Format(entryDateLayout)},
		inputValue(t, body, "to"), "to default")
	wantMonthStarts := []string{
		time.Date(before.Year(), before.Month(), 1, 0, 0, 0, 0, time.UTC).Format(entryDateLayout),
		time.Date(after.Year(), after.Month(), 1, 0, 0, 0, 0, time.UTC).Format(entryDateLayout),
	}
	assert.Contains(t, wantMonthStarts, inputValue(t, body, "from"), "from default")
}
