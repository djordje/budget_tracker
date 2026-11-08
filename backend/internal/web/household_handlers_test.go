package web

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"budgettracker/internal/auth"
	"budgettracker/internal/households"
)

type stubSessionLookup struct {
	session auth.Session
}

func (s stubSessionLookup) SessionByToken(context.Context, string) (auth.Session, error) {
	return s.session, nil
}

func createHouseholdRequest(t *testing.T, session auth.Session, handler http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/app/households", strings.NewReader("name=Home"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{Name: auth.DefaultSessionCookieName, Value: "raw"})
	recorder := httptest.NewRecorder()
	auth.NewMiddleware(stubSessionLookup{session: session}, auth.CookieConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil))).
		Load(handler).ServeHTTP(recorder, request)
	return recorder
}

func TestCreateHouseholdUsesAtomicWorkflow(t *testing.T) {
	userID := uuid.New()
	tokenHash := strings.Repeat("a", 64)
	household := households.Household{ID: uuid.New(), Name: "Home", Role: households.RoleAdmin}
	var called int

	handler, err := New(Deps{
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Sessions: stubSessions{},
		Households: stubHouseholds{
			createAndActivate: func(_ context.Context, actorID uuid.UUID, sessionHash, name string) (households.Household, error) {
				called++
				if actorID != userID || sessionHash != tokenHash || name != "Home" {
					t.Errorf("CreateAndActivate(%s, %q, %q)", actorID, sessionHash, name)
				}
				return household, nil
			},
		},
		Entries: stubEntries{},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	recorder := createHouseholdRequest(t,
		auth.Session{UserID: userID, User: auth.User{ID: userID}, TokenHash: tokenHash},
		http.HandlerFunc(handler.CreateHousehold))

	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusSeeOther, recorder.Body.String())
	}
	if got := recorder.Header().Get("Location"); got != "/app?notice=household_created" {
		t.Errorf("Location = %q", got)
	}
	if called != 1 {
		t.Errorf("CreateAndActivate calls = %d, want 1", called)
	}
}

func TestCreateHouseholdRendersValidationErrors(t *testing.T) {
	userID := uuid.New()
	handler, err := New(Deps{
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Sessions: stubSessions{},
		Households: stubHouseholds{
			createAndActivate: func(context.Context, uuid.UUID, string, string) (households.Household, error) {
				return households.Household{}, &households.ValidationError{
					Field:   "name",
					Message: "Household name is required.",
				}
			},
		},
		Entries: stubEntries{},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	recorder := createHouseholdRequest(t,
		auth.Session{UserID: userID, User: auth.User{ID: userID}, TokenHash: strings.Repeat("a", 64)},
		http.HandlerFunc(handler.CreateHousehold))

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnprocessableEntity)
	}
	if recorder.Header().Get("Location") != "" {
		t.Error("validation failure returned a redirect")
	}
	if !strings.Contains(recorder.Body.String(), "Household name is required.") {
		t.Errorf("body does not contain the validation message:\n%s", recorder.Body.String())
	}
}

func TestCreateHouseholdRendersActivationFailure(t *testing.T) {
	userID := uuid.New()
	handler, err := New(Deps{
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Sessions: stubSessions{},
		Households: stubHouseholds{
			createAndActivate: func(context.Context, uuid.UUID, string, string) (households.Household, error) {
				return households.Household{}, auth.ErrSessionNotFound
			},
		},
		Entries: stubEntries{},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	recorder := createHouseholdRequest(t,
		auth.Session{UserID: userID, User: auth.User{ID: userID}, TokenHash: strings.Repeat("a", 64)},
		http.HandlerFunc(handler.CreateHousehold))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if recorder.Header().Get("Location") != "" {
		t.Error("activation failure returned a redirect")
	}
	if !strings.Contains(recorder.Body.String(), "Session expired") {
		t.Errorf("body does not render the session error:\n%s", recorder.Body.String())
	}
}
