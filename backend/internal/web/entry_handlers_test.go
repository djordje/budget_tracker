package web

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

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

			if !entries.called {
				t.Fatal("Delete() was not called")
			}
			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.wantStatus)
			}
			body := recorder.Body.String()
			if !strings.Contains(body, `id="entries-content"`) {
				t.Errorf("body is not an entries-content fragment:\n%s", body)
			}
			if !strings.Contains(body, tt.wantText) {
				t.Errorf("body does not contain %q:\n%s", tt.wantText, body)
			}
			if strings.Contains(body, "notice=entry_deleted") || strings.Contains(body, "Entry deleted") {
				t.Errorf("failure fragment advertised a successful deletion:\n%s", body)
			}
			if strings.Contains(body, "<html") {
				t.Error("failure returned a full page instead of a fragment")
			}
		})
	}
}
