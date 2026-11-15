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
)

type failingDeleteEntryService struct {
	EntryService
	err    error
	called bool
}

func (s *failingDeleteEntryService) Delete(context.Context, uuid.UUID, finance.Kind, int64) error {
	s.called = true
	return s.err
}

func serveHTMXDelete(t *testing.T, entries EntryService) *httptest.ResponseRecorder {
	t.Helper()
	handler, err := New(Deps{
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Sessions:   stubSessions{},
		Households: stubHouseholds{},
		Entries:    entries,
		Rates:      stubRates{},
		Reports:    stubReports{},
	})
	require.NoError(t, err, "New()")

	userID := uuid.New()
	session := auth.Session{UserID: userID, User: auth.User{ID: userID}}
	request := httptest.NewRequest(http.MethodPost, "/app/spendings/42/delete", nil)
	request.SetPathValue("entryID", "42")
	request.Header.Set("HX-Request", "true")
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
			entries := &failingDeleteEntryService{err: tt.err}
			recorder := serveHTMXDelete(t, entries)

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
