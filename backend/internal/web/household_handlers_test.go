package web

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
			createAndActivate: func(_ context.Context, params households.CreateParams) (households.Household, error) {
				called++
				assert.Equal(t, userID, params.UserID, "CreateAndActivate() actor")
				assert.Equal(t, tokenHash, params.SessionTokenHash, "CreateAndActivate() session hash")
				assert.Equal(t, "Home", params.Name, "CreateAndActivate() name")
				return household, nil
			},
		},
		Entries: stubEntries{},
		Rates:   stubRates{},
		Reports: stubReports{},
	})
	require.NoError(t, err, "New()")

	recorder := createHouseholdRequest(t,
		auth.Session{UserID: userID, User: auth.User{ID: userID}, TokenHash: tokenHash},
		http.HandlerFunc(handler.CreateHousehold))

	require.Equal(t, http.StatusSeeOther, recorder.Code, "body = %s", recorder.Body.String())
	assert.Equal(t, householdURL(household.ID, "/entries?notice=household_created"), recorder.Header().Get("Location"), "Location")
	assert.Equal(t, 1, called, "CreateAndActivate() calls")
}

func TestCreateHouseholdRendersValidationErrors(t *testing.T) {
	userID := uuid.New()
	handler, err := New(Deps{
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Sessions: stubSessions{},
		Households: stubHouseholds{
			createAndActivate: func(context.Context, households.CreateParams) (households.Household, error) {
				return households.Household{}, &households.ValidationError{
					Field:   "name",
					Message: "Household name is required.",
				}
			},
		},
		Entries: stubEntries{},
		Rates:   stubRates{},
		Reports: stubReports{},
	})
	require.NoError(t, err, "New()")

	recorder := createHouseholdRequest(t,
		auth.Session{UserID: userID, User: auth.User{ID: userID}, TokenHash: strings.Repeat("a", 64)},
		http.HandlerFunc(handler.CreateHousehold))

	require.Equal(t, http.StatusUnprocessableEntity, recorder.Code, "body = %s", recorder.Body.String())
	assert.Empty(t, recorder.Header().Get("Location"), "validation failure returned a redirect")
	assert.Contains(t, recorder.Body.String(), "Household name is required.")
}

func TestCreateHouseholdRendersActivationFailure(t *testing.T) {
	userID := uuid.New()
	handler, err := New(Deps{
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Sessions: stubSessions{},
		Households: stubHouseholds{
			createAndActivate: func(context.Context, households.CreateParams) (households.Household, error) {
				return households.Household{}, auth.ErrSessionNotFound
			},
		},
		Entries: stubEntries{},
		Rates:   stubRates{},
		Reports: stubReports{},
	})
	require.NoError(t, err, "New()")

	recorder := createHouseholdRequest(t,
		auth.Session{UserID: userID, User: auth.User{ID: userID}, TokenHash: strings.Repeat("a", 64)},
		http.HandlerFunc(handler.CreateHousehold))

	require.Equal(t, http.StatusUnauthorized, recorder.Code, "body = %s", recorder.Body.String())
	assert.Empty(t, recorder.Header().Get("Location"), "activation failure returned a redirect")
	assert.Contains(t, recorder.Body.String(), "Session expired")
}

func TestMalformedHouseholdIDRendersErrorPage(t *testing.T) {
	handler := newTestHandler(t)
	tests := []struct {
		name   string
		method string
		path   string
		handle http.HandlerFunc
	}{
		{name: "settings", method: http.MethodGet, path: "/app/households/not-a-uuid/settings", handle: handler.HouseholdSettings},
		{name: "update", method: http.MethodPost, path: "/app/households/not-a-uuid/settings", handle: handler.UpdateHousehold},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(tt.method, tt.path, nil)
			request.SetPathValue("householdID", "not-a-uuid")
			recorder := httptest.NewRecorder()

			tt.handle(recorder, request)

			require.Equal(t, http.StatusNotFound, recorder.Code, "body = %s", recorder.Body.String())
			assert.Equal(t, "text/html; charset=utf-8", recorder.Header().Get("Content-Type"), "Content-Type")
			body := recorder.Body.String()
			assert.Contains(t, body, `<html lang="en"`, "error response is not the styled page")
			assert.Contains(t, body, "Household not found", "error response copy")
		})
	}
}

func TestHouseholdSettingsLabelsExpiredInvitations(t *testing.T) {
	household := households.Household{ID: uuid.New(), Name: "Home", Role: households.RoleAdmin}
	expiredAt := time.Date(2026, time.January, 5, 12, 0, 0, 0, time.UTC)
	expiresAt := time.Date(2099, time.December, 5, 12, 0, 0, 0, time.UTC)
	handler, err := New(Deps{
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Sessions: stubSessions{},
		Households: stubHouseholds{
			households: []households.Household{household},
			invitations: []households.Invitation{
				{ID: uuid.New(), HouseholdID: household.ID, Email: "expired@example.com", Role: households.RoleMember, ExpiresAt: expiredAt},
				{ID: uuid.New(), HouseholdID: household.ID, Email: "pending@example.com", Role: households.RoleMember, ExpiresAt: expiresAt},
			},
		},
		Entries: stubEntries{},
		Rates:   stubRates{},
		Reports: stubReports{},
	})
	require.NoError(t, err, "New()")

	userID := uuid.New()
	session := auth.Session{
		UserID:            userID,
		User:              auth.User{ID: userID},
		ActiveHouseholdID: &household.ID,
	}
	request := httptest.NewRequest(http.MethodGet, householdURL(household.ID, "/settings"), nil)
	request.SetPathValue("householdID", household.ID.String())
	request.AddCookie(&http.Cookie{Name: auth.DefaultSessionCookieName, Value: "raw"})
	recorder := httptest.NewRecorder()
	auth.NewMiddleware(stubSessionLookup{session: session}, auth.CookieConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil))).
		Load(handler.Scoped("more", "settings", handler.HouseholdSettings)).ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code, "body = %s", recorder.Body.String())
	body := recorder.Body.String()
	assert.Contains(t, body, "expired@example.com", "expired invitation is not listed")
	assert.Contains(t, body, "Expired on 5 January 2026", "expired invitation is not labelled expired")
	assert.Contains(t, body, "pending@example.com", "pending invitation is not listed")
	assert.Contains(t, body, "Expires 5 December 2099", "pending invitation is not labelled with its expiry")
}
