package web

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
			createAndActivate: func(_ context.Context, actorID uuid.UUID, sessionHash, name string) (households.Household, error) {
				called++
				assert.Equal(t, userID, actorID, "CreateAndActivate() actor")
				assert.Equal(t, tokenHash, sessionHash, "CreateAndActivate() session hash")
				assert.Equal(t, "Home", name, "CreateAndActivate() name")
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
	assert.Equal(t, "/app?notice=household_created", recorder.Header().Get("Location"), "Location")
	assert.Equal(t, 1, called, "CreateAndActivate() calls")
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
			createAndActivate: func(context.Context, uuid.UUID, string, string) (households.Household, error) {
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
