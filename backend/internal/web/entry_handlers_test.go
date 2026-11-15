package web

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/google/uuid"

	"budgettracker/internal/auth"
	"budgettracker/internal/finance"
	"budgettracker/internal/households"
)

type failingDeleteEntryService struct {
	EntryService
	err    error
	entry  finance.Entry
	called bool
}

func (s *failingDeleteEntryService) Get(context.Context, uuid.UUID, finance.Kind, int64) (finance.Entry, error) {
	return s.entry, nil
}

func (s *failingDeleteEntryService) Delete(context.Context, uuid.UUID, finance.Kind, int64) error {
	s.called = true
	return s.err
}

type scopedDeleteEntryService struct {
	EntryService
	entry       finance.Entry
	deleteCalls int
}

func (s *scopedDeleteEntryService) Get(context.Context, uuid.UUID, finance.Kind, int64) (finance.Entry, error) {
	return s.entry, nil
}

func (s *scopedDeleteEntryService) Delete(context.Context, uuid.UUID, finance.Kind, int64) error {
	s.deleteCalls++
	return nil
}

func serveEntryDelete(
	t *testing.T,
	householdsService HouseholdService,
	entries EntryService,
	htmx bool,
) *httptest.ResponseRecorder {
	t.Helper()
	handler, err := New(Deps{
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Sessions:   stubSessions{},
		Households: householdsService,
		Entries:    entries,
		Rates:      stubRates{},
		Reports:    stubReports{},
	})
	require.NoError(t, err, "New()")

	userID := uuid.New()
	session := auth.Session{UserID: userID, User: auth.User{ID: userID}}
	request := httptest.NewRequest(http.MethodPost, "/app/spendings/42/delete", nil)
	request.SetPathValue("entryID", "42")
	if htmx {
		request.Header.Set("HX-Request", "true")
	}
	request.AddCookie(&http.Cookie{Name: auth.DefaultSessionCookieName, Value: "raw"})
	recorder := httptest.NewRecorder()
	auth.NewMiddleware(stubSessionLookup{session: session}, auth.CookieConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil))).
		Load(http.HandlerFunc(handler.DeleteSpending)).ServeHTTP(recorder, request)
	return recorder
}

func TestHTMXDeleteFailuresReturnErrorFragments(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantText   string
	}{
		{name: "forbidden", err: finance.ErrForbidden, wantStatus: http.StatusForbidden, wantText: "do not have permission"},
		{name: "missing", err: finance.ErrNotFound, wantStatus: http.StatusNotFound, wantText: "no longer available"},
		{name: "internal", err: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError, wantText: "could not be deleted"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			household := households.Household{ID: uuid.New(), Name: "Home", Role: households.RoleAdmin}
			entries := &failingDeleteEntryService{
				err:   tt.err,
				entry: finance.Entry{ID: 42, Kind: finance.KindSpending, HouseholdID: household.ID},
			}
			recorder := serveEntryDelete(t, stubHouseholds{households: []households.Household{household}}, entries, true)

			assert.True(t, entries.called, "Delete() was not called")
			require.Equal(t, tt.wantStatus, recorder.Code, "body = %s", recorder.Body.String())
			body := recorder.Body.String()
			assert.Contains(t, body, `id="entries-content"`, "body is not an entries-content fragment")
			assert.Contains(t, body, tt.wantText)
			assert.NotContains(t, body, "notice=entry_deleted", "failure fragment advertised a successful deletion")
			assert.NotContains(t, body, "Entry deleted", "failure fragment advertised a successful deletion")
			assert.NotContains(t, body, "<html", "failure returned a full page instead of a fragment")
		})
	}
}

func TestDeleteRejectsEntryFromAnotherHousehold(t *testing.T) {
	active := households.Household{ID: uuid.New(), Name: "Home", Role: households.RoleAdmin}
	other := households.Household{ID: uuid.New(), Name: "Other", Role: households.RoleAdmin}
	tests := []struct {
		name     string
		htmx     bool
		wantText string
	}{
		{name: "page", wantText: "That entry is not available."},
		{name: "fragment", htmx: true, wantText: "That entry is no longer available."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries := &scopedDeleteEntryService{entry: finance.Entry{
				ID:          42,
				Kind:        finance.KindSpending,
				HouseholdID: other.ID,
			}}
			recorder := serveEntryDelete(t, stubHouseholds{households: []households.Household{active}}, entries, tt.htmx)

			require.Equal(t, http.StatusNotFound, recorder.Code, "body = %s", recorder.Body.String())
			assert.Equal(t, 0, entries.deleteCalls, "Delete() calls")
			assert.Contains(t, recorder.Body.String(), tt.wantText)
		})
	}
}
