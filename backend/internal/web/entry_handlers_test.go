package web

import (
	"context"
	"errors"
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
		name     string
		err      error
		wantText string
	}{
		{name: "forbidden", err: finance.ErrForbidden, wantText: "do not have permission"},
		{name: "missing", err: finance.ErrNotFound, wantText: "no longer available"},
		{name: "internal", err: errors.New("database unavailable"), wantText: "could not be deleted"},
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
			require.Equal(t, http.StatusOK, recorder.Code, "body = %s", recorder.Body.String())
			body := recorder.Body.String()
			assert.Contains(t, body, `id="entries-content"`, "body is not an entries-content fragment")
			assert.Contains(t, body, tt.wantText)
			assert.NotContains(t, body, "notice=entry_deleted", "failure fragment advertised a successful deletion")
			assert.NotContains(t, body, "Entry deleted", "failure fragment advertised a successful deletion")
			assert.NotContains(t, body, "<html", "failure returned a full page instead of a fragment")
		})
	}
}

func TestDeleteFailuresKeepRealStatusOnFullPages(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{name: "forbidden", err: finance.ErrForbidden, wantStatus: http.StatusForbidden},
		{name: "missing", err: finance.ErrNotFound, wantStatus: http.StatusNotFound},
		{name: "internal", err: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			household := households.Household{ID: uuid.New(), Name: "Home", Role: households.RoleAdmin}
			entries := &failingDeleteEntryService{
				err:   tt.err,
				entry: finance.Entry{ID: 42, Kind: finance.KindSpending, HouseholdID: household.ID},
			}
			recorder := serveEntryDelete(t, stubHouseholds{households: []households.Household{household}}, entries, false)

			require.Equal(t, tt.wantStatus, recorder.Code, "body = %s", recorder.Body.String())
			assert.Contains(t, recorder.Body.String(), "<html", "failure did not return a full page")
		})
	}
}

func TestDeleteRejectsEntryFromAnotherHousehold(t *testing.T) {
	active := households.Household{ID: uuid.New(), Name: "Home", Role: households.RoleAdmin}
	other := households.Household{ID: uuid.New(), Name: "Other", Role: households.RoleAdmin}
	tests := []struct {
		name       string
		htmx       bool
		wantStatus int
		wantText   string
	}{
		{name: "page", wantStatus: http.StatusNotFound, wantText: "That entry is not available."},
		{name: "fragment", htmx: true, wantStatus: http.StatusOK, wantText: "That entry is no longer available."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries := &scopedDeleteEntryService{entry: finance.Entry{
				ID:          42,
				Kind:        finance.KindSpending,
				HouseholdID: other.ID,
			}}
			recorder := serveEntryDelete(t, stubHouseholds{households: []households.Household{active}}, entries, tt.htmx)

			require.Equal(t, tt.wantStatus, recorder.Code, "body = %s", recorder.Body.String())
			assert.Equal(t, 0, entries.deleteCalls, "Delete() calls")
			assert.Contains(t, recorder.Body.String(), tt.wantText)
		})
	}
}

func TestParseFormDateMatchesTheSharedParser(t *testing.T) {
	valid := time.Date(2026, time.October, 15, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		raw     string
		want    time.Time
		wantErr bool
	}{
		{name: "valid", raw: "2026-10-15", want: valid},
		{name: "empty", raw: ""},
		{name: "padded", raw: " 2026-10-15", wantErr: true},
		{name: "whitespace-only", raw: "   ", wantErr: true},
		{name: "malformed", raw: "15/10/2026", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseFormDate(tt.raw)

			if tt.wantErr {
				assert.Error(t, err, "parseFormDate(%q)", tt.raw)
				return
			}
			require.NoError(t, err, "parseFormDate(%q)", tt.raw)
			assert.Equal(t, tt.want, got, "parseFormDate(%q)", tt.raw)
		})
	}
}

func TestParsePageMatchesTheJSONAdapter(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    int
		wantErr bool
	}{
		{name: "empty uses default", raw: "", want: 1},
		{name: "zero uses default", raw: "0", want: 0},
		{name: "first page", raw: "1", want: 1},
		{name: "negative", raw: "-1", wantErr: true},
		{name: "non-numeric", raw: "abc", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePage(tt.raw)

			if tt.wantErr {
				assert.Error(t, err, "parsePage(%q)", tt.raw)
				return
			}
			require.NoError(t, err, "parsePage(%q)", tt.raw)
			assert.Equal(t, tt.want, got, "parsePage(%q)", tt.raw)
		})
	}
}

func TestNewEntryFormDefaultsToUTCDate(t *testing.T) {
	before := finance.Today()
	recorder := serveHouseholdPage(t, func(h *Handler) http.HandlerFunc { return h.NewIncomePage })
	// A request can straddle UTC midnight, so accept the date observed on
	// either side of it instead of racing the clock.
	after := finance.Today()

	require.Equal(t, http.StatusOK, recorder.Code, "body = %s", recorder.Body.String())
	wantDates := []string{before.Format(entryDateLayout), after.Format(entryDateLayout)}
	assert.Contains(t, wantDates, inputValue(t, recorder.Body.String(), "entry_date"), "entry date default")
}
