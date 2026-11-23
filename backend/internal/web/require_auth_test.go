package web

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/google/uuid"

	"budgettracker/internal/auth"
	"budgettracker/internal/finance"
	"budgettracker/internal/households"
)

// serviceCalls counts the service methods a handler reaches. The wrapped route
// tests use it to show that RequireAuth stops an anonymous request before any
// handler work happens.
type serviceCalls struct {
	createHousehold  int
	rename           int
	activate         int
	updateOptions    int
	createInvitation int
	revokeInvitation int
	acceptInvitation int
	list             int
	createEntry      int
	getEntry         int
	updateEntry      int
	updateRate       int
	deleteRate       int
}

func (c *serviceCalls) total() int {
	return c.createHousehold + c.rename + c.activate + c.updateOptions +
		c.createInvitation + c.revokeInvitation + c.acceptInvitation +
		c.list + c.createEntry + c.getEntry + c.updateEntry + c.updateRate + c.deleteRate
}

type countingHouseholds struct {
	stubHouseholds
	calls *serviceCalls
}

func (s countingHouseholds) CreateAndActivate(
	ctx context.Context, userID uuid.UUID, sessionTokenHash, name string,
) (households.Household, error) {
	s.calls.createHousehold++
	return s.stubHouseholds.CreateAndActivate(ctx, userID, sessionTokenHash, name)
}

func (s countingHouseholds) Rename(ctx context.Context, userID, householdID uuid.UUID, name string) (households.Household, error) {
	s.calls.rename++
	return s.stubHouseholds.Rename(ctx, userID, householdID, name)
}

func (s countingHouseholds) UpdateOptions(
	ctx context.Context, actorID, householdID uuid.UUID, params households.UpdateOptionsParams,
) (households.Options, error) {
	s.calls.updateOptions++
	return s.stubHouseholds.UpdateOptions(ctx, actorID, householdID, params)
}

func (s countingHouseholds) CreateInvitation(
	ctx context.Context, params households.CreateInvitationParams,
) (households.Invitation, string, error) {
	s.calls.createInvitation++
	return s.stubHouseholds.CreateInvitation(ctx, params)
}

func (s countingHouseholds) RevokeInvitation(ctx context.Context, actorID, householdID, invitationID uuid.UUID) error {
	s.calls.revokeInvitation++
	return s.stubHouseholds.RevokeInvitation(ctx, actorID, householdID, invitationID)
}

func (s countingHouseholds) AcceptInvitation(ctx context.Context, rawToken string, userID uuid.UUID) (households.Invitation, error) {
	s.calls.acceptInvitation++
	return s.stubHouseholds.AcceptInvitation(ctx, rawToken, userID)
}

func (s countingHouseholds) List(ctx context.Context, userID uuid.UUID) ([]households.Household, error) {
	s.calls.list++
	return s.stubHouseholds.List(ctx, userID)
}

type countingEntries struct {
	stubEntries
	calls *serviceCalls
}

func (s countingEntries) Create(ctx context.Context, params finance.CreateParams) (finance.Entry, error) {
	s.calls.createEntry++
	return s.stubEntries.Create(ctx, params)
}

func (s countingEntries) Get(ctx context.Context, actorID uuid.UUID, kind finance.Kind, id int64) (finance.Entry, error) {
	s.calls.getEntry++
	return s.stubEntries.Get(ctx, actorID, kind, id)
}

func (s countingEntries) Update(ctx context.Context, params finance.UpdateParams) (finance.Entry, error) {
	s.calls.updateEntry++
	return s.stubEntries.Update(ctx, params)
}

type countingRates struct {
	stubRates
	calls *serviceCalls
}

func (s countingRates) UpdateExchangeRate(
	ctx context.Context, params finance.UpdateExchangeRateParams,
) (finance.ExchangeRate, error) {
	s.calls.updateRate++
	return s.stubRates.UpdateExchangeRate(ctx, params)
}

func (s countingRates) DeleteExchangeRate(ctx context.Context, actorID uuid.UUID, rateID int64) error {
	s.calls.deleteRate++
	return s.stubRates.DeleteExchangeRate(ctx, actorID, rateID)
}

type countingSessions struct {
	stubSessions
	calls *serviceCalls
}

func (s countingSessions) SetActiveHousehold(
	ctx context.Context, tokenHash string, userID uuid.UUID, householdID *uuid.UUID,
) error {
	s.calls.activate++
	return s.stubSessions.SetActiveHousehold(ctx, tokenHash, userID, householdID)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func countingHandler(t *testing.T, householdID uuid.UUID, calls *serviceCalls) *Handler {
	t.Helper()
	handler, err := New(Deps{
		Logger:   discardLogger(),
		Sessions: countingSessions{calls: calls},
		Households: countingHouseholds{
			stubHouseholds: stubHouseholds{
				households: []households.Household{{ID: householdID, Name: "Home", Role: households.RoleAdmin}},
			},
			calls: calls,
		},
		Entries: countingEntries{calls: calls},
		Rates:   countingRates{calls: calls},
		Reports: stubReports{},
	})
	require.NoError(t, err, "New()")
	return handler
}

// TestRequireAuthWrappedHandlers verifies that each protected handler that no
// longer checks for a session itself is still guarded by RequireAuth, and that
// the guard does not prevent an authenticated request from reaching the
// handler.
func TestRequireAuthWrappedHandlers(t *testing.T) {
	householdID := uuid.New()
	invitationID := uuid.New()

	tests := []struct {
		name       string
		method     string
		path       string
		form       string
		pathValues map[string]string
		handle     func(*Handler) http.HandlerFunc
	}{
		{
			name:   "app landing",
			method: http.MethodGet,
			path:   "/app",
			handle: func(h *Handler) http.HandlerFunc { return h.App },
		},
		{
			name:   "create household",
			method: http.MethodPost,
			path:   "/app/households",
			form:   "name=Home",
			handle: func(h *Handler) http.HandlerFunc { return h.CreateHousehold },
		},
		{
			name:       "update household",
			method:     http.MethodPost,
			path:       "/app/households/" + householdID.String() + "/settings",
			form:       "name=Home",
			pathValues: map[string]string{"householdID": householdID.String()},
			handle:     func(h *Handler) http.HandlerFunc { return h.UpdateHousehold },
		},
		{
			name:   "activate household",
			method: http.MethodPost,
			path:   "/app/active-household",
			form:   "household_id=" + householdID.String(),
			handle: func(h *Handler) http.HandlerFunc { return h.ActivateHousehold },
		},
		{
			name:       "update household options",
			method:     http.MethodPost,
			path:       "/app/households/" + householdID.String() + "/options",
			pathValues: map[string]string{"householdID": householdID.String()},
			handle:     func(h *Handler) http.HandlerFunc { return h.UpdateHouseholdOptions },
		},
		{
			name:       "create invitation",
			method:     http.MethodPost,
			path:       "/app/households/" + householdID.String() + "/invitations",
			form:       "email=ada%40example.com&role=member",
			pathValues: map[string]string{"householdID": householdID.String()},
			handle:     func(h *Handler) http.HandlerFunc { return h.CreateHouseholdInvitation },
		},
		{
			name:   "revoke invitation",
			method: http.MethodPost,
			path:   "/app/households/" + householdID.String() + "/invitations/" + invitationID.String() + "/delete",
			pathValues: map[string]string{
				"householdID":  householdID.String(),
				"invitationID": invitationID.String(),
			},
			handle: func(h *Handler) http.HandlerFunc { return h.RevokeHouseholdInvitation },
		},
		{
			name:   "create income",
			method: http.MethodPost,
			path:   "/app/incomes",
			form:   "amount=1&currency_code=EUR&entry_date=2026-10-15",
			handle: func(h *Handler) http.HandlerFunc { return h.CreateIncome },
		},
		{
			name:       "edit income",
			method:     http.MethodGet,
			path:       "/app/incomes/42/edit",
			pathValues: map[string]string{"entryID": "42"},
			handle:     func(h *Handler) http.HandlerFunc { return h.EditIncomePage },
		},
		{
			name:       "update income",
			method:     http.MethodPost,
			path:       "/app/incomes/42",
			form:       "amount=1&currency_code=EUR&entry_date=2026-10-15",
			pathValues: map[string]string{"entryID": "42"},
			handle:     func(h *Handler) http.HandlerFunc { return h.UpdateIncome },
		},
		{
			name:       "delete income",
			method:     http.MethodPost,
			path:       "/app/incomes/42/delete",
			pathValues: map[string]string{"entryID": "42"},
			handle:     func(h *Handler) http.HandlerFunc { return h.DeleteIncome },
		},
		{
			name:   "update preferences",
			method: http.MethodPost,
			path:   "/app/preferences",
			form:   "default_income_currency_code=EUR",
			handle: func(h *Handler) http.HandlerFunc { return h.UpdatePreferences },
		},
		{
			name:   "create exchange rate",
			method: http.MethodPost,
			path:   "/app/exchange-rates",
			form:   "from_currency_code=EUR&to_currency_code=RSD&rate=1&rate_date=2026-10-15",
			handle: func(h *Handler) http.HandlerFunc { return h.CreateExchangeRate },
		},
		{
			name:       "edit exchange rate",
			method:     http.MethodGet,
			path:       "/app/exchange-rates/7/edit",
			pathValues: map[string]string{"rateID": "7"},
			handle:     func(h *Handler) http.HandlerFunc { return h.EditExchangeRatePage },
		},
		{
			name:       "update exchange rate",
			method:     http.MethodPost,
			path:       "/app/exchange-rates/7",
			form:       "from_currency_code=EUR&to_currency_code=RSD&rate=1&rate_date=2026-10-15",
			pathValues: map[string]string{"rateID": "7"},
			handle:     func(h *Handler) http.HandlerFunc { return h.UpdateExchangeRate },
		},
		{
			name:       "delete exchange rate",
			method:     http.MethodPost,
			path:       "/app/exchange-rates/7/delete",
			pathValues: map[string]string{"rateID": "7"},
			handle:     func(h *Handler) http.HandlerFunc { return h.DeleteExchangeRate },
		},
		{
			name:   "reports",
			method: http.MethodGet,
			path:   "/app/reports",
			handle: func(h *Handler) http.HandlerFunc { return h.ReportsPage },
		},
	}

	userID := uuid.New()
	session := auth.Session{
		UserID:            userID,
		User:              auth.User{ID: userID},
		TokenHash:         strings.Repeat("a", 64),
		ActiveHouseholdID: &householdID,
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scoped := tt.path != "/app" && tt.path != "/app/households" && tt.path != "/app/active-household"
			wrap := func(h *Handler) http.HandlerFunc {
				if scoped {
					return h.Scoped("", "", tt.handle(h))
				}
				return RequireAuth(tt.handle(h))
			}
			newRequest := func() *http.Request {
				request := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.form))
				if tt.form != "" {
					request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				}
				if scoped {
					request.SetPathValue("householdID", householdID.String())
				}
				for name, value := range tt.pathValues {
					request.SetPathValue(name, value)
				}
				return request
			}

			anonymousCalls := &serviceCalls{}
			anonymousHandler := countingHandler(t, householdID, anonymousCalls)
			recorder := httptest.NewRecorder()
			auth.NewMiddleware(stubSessionLookup{}, auth.CookieConfig{}, discardLogger()).
				Load(wrap(anonymousHandler)).
				ServeHTTP(recorder, newRequest())

			require.Equal(t, http.StatusSeeOther, recorder.Code, "anonymous request body = %s", recorder.Body.String())
			wantLocation := "/login"
			if tt.method == http.MethodGet {
				wantLocation += "?next=" + url.QueryEscape(tt.path)
			}
			assert.Equal(t, wantLocation, recorder.Header().Get("Location"), "anonymous request Location")
			assert.Zero(t, anonymousCalls.total(), "anonymous request reached the services")

			authenticatedCalls := &serviceCalls{}
			authenticatedHandler := countingHandler(t, householdID, authenticatedCalls)
			request := newRequest()
			request.AddCookie(&http.Cookie{Name: auth.DefaultSessionCookieName, Value: "raw"})
			recorder = httptest.NewRecorder()
			auth.NewMiddleware(stubSessionLookup{session: session}, auth.CookieConfig{}, discardLogger()).
				Load(wrap(authenticatedHandler)).
				ServeHTTP(recorder, request)

			assert.Positive(t, authenticatedCalls.total(),
				"authenticated request did not reach the services; status %d body = %s",
				recorder.Code, recorder.Body.String())
			assert.NotEqual(t, "/login", recorder.Header().Get("Location"), "authenticated request was redirected to sign in")
		})
	}
}

// TestAcceptInvitationRedirectsAnonymousToNext checks the invitation-aware
// exception: acceptance owns its anonymous redirect so the visitor returns to
// the invitation after signing in.
func TestAcceptInvitationRedirectsAnonymousToNext(t *testing.T) {
	calls := &serviceCalls{}
	handler := countingHandler(t, uuid.New(), calls)
	request := httptest.NewRequest(http.MethodPost, "/invitations/token", nil)
	request.SetPathValue("token", "token")
	recorder := httptest.NewRecorder()

	handler.AcceptInvitation(recorder, request)

	require.Equal(t, http.StatusSeeOther, recorder.Code, "body = %s", recorder.Body.String())
	assert.Equal(t, "/login?next=%2Finvitations%2Ftoken", recorder.Header().Get("Location"), "Location")
	assert.Zero(t, calls.acceptInvitation, "AcceptInvitation ran for an anonymous request")
}
