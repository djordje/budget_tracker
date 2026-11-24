package httpapi

import (
	"context"
	"encoding/json"
	"errors"
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

type stubHouseholdService struct {
	HouseholdService
	createAndActivate func(context.Context, households.CreateParams) (households.Household, error)
	list              func(context.Context, uuid.UUID) ([]households.Household, error)
	get               func(context.Context, uuid.UUID, uuid.UUID) (households.Household, error)
	members           func(context.Context, uuid.UUID, uuid.UUID) ([]households.Member, error)
	getOptions        func(context.Context, uuid.UUID, uuid.UUID) (households.Options, error)
	getOptionsView    func(context.Context, uuid.UUID, uuid.UUID) (households.OptionsView, error)
	setUserOptions    func(context.Context, uuid.UUID, uuid.UUID, households.UpdateUserOptionsParams) (households.OptionsView, error)
	revokeInvitation  func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error
}

func (s stubHouseholdService) CreateAndActivate(
	ctx context.Context, params households.CreateParams,
) (households.Household, error) {
	return s.createAndActivate(ctx, params)
}

func (s stubHouseholdService) List(ctx context.Context, userID uuid.UUID) ([]households.Household, error) {
	return s.list(ctx, userID)
}

func (s stubHouseholdService) Get(ctx context.Context, userID, householdID uuid.UUID) (households.Household, error) {
	return s.get(ctx, userID, householdID)
}

func (s stubHouseholdService) Members(ctx context.Context, userID, householdID uuid.UUID) ([]households.Member, error) {
	return s.members(ctx, userID, householdID)
}

func (s stubHouseholdService) GetOptions(ctx context.Context, userID, householdID uuid.UUID) (households.Options, error) {
	return s.getOptions(ctx, userID, householdID)
}

func (s stubHouseholdService) GetOptionsView(ctx context.Context, userID, householdID uuid.UUID) (households.OptionsView, error) {
	return s.getOptionsView(ctx, userID, householdID)
}

func (s stubHouseholdService) SetUserOptions(
	ctx context.Context, userID, householdID uuid.UUID, params households.UpdateUserOptionsParams,
) (households.OptionsView, error) {
	return s.setUserOptions(ctx, userID, householdID, params)
}

func (s stubHouseholdService) RevokeInvitation(
	ctx context.Context, actorID, householdID, invitationID uuid.UUID,
) error {
	return s.revokeInvitation(ctx, actorID, householdID, invitationID)
}

type stubSessionLookup struct {
	session auth.Session
}

func (s stubSessionLookup) SessionByToken(context.Context, string) (auth.Session, error) {
	return s.session, nil
}

func sessionRequest(t *testing.T, session auth.Session, handler http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.AddCookie(&http.Cookie{Name: auth.DefaultSessionCookieName, Value: "raw"})
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	auth.NewMiddleware(stubSessionLookup{session: session}, auth.CookieConfig{}, discardLogger()).
		Load(handler).ServeHTTP(recorder, request)
	return recorder
}

func TestHouseholdHandlerRequiresAuthentication(t *testing.T) {
	handler := NewHouseholdHandler(stubHouseholdService{}, discardLogger())
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/households", nil)

	RequireAuthAPI(handler.List)(recorder, request)

	require.Equal(t, http.StatusUnauthorized, recorder.Code, "body = %s", recorder.Body.String())

	var body ErrorResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body), "decode response")
	assert.Equal(t, "unauthenticated", body.Error.Code, "error code")
}

func TestHouseholdHandlerCreateCommitsBeforeSuccess(t *testing.T) {
	userID := uuid.New()
	tokenHash := strings.Repeat("a", 64)
	household := households.Household{
		ID:   uuid.New(),
		Name: "Home",
		Role: households.RoleAdmin,
	}
	var gotHash string
	handler := NewHouseholdHandler(stubHouseholdService{
		createAndActivate: func(_ context.Context, params households.CreateParams) (households.Household, error) {
			assert.Equal(t, userID, params.UserID, "CreateAndActivate() actor")
			assert.Equal(t, "Home", params.Name, "CreateAndActivate() name")
			gotHash = params.SessionTokenHash
			return household, nil
		},
	}, discardLogger())

	recorder := sessionRequest(t, auth.Session{UserID: userID, User: auth.User{ID: userID}, TokenHash: tokenHash}, http.HandlerFunc(handler.Create),
		http.MethodPost, "/api/v1/households", `{"name":"Home"}`)

	require.Equal(t, http.StatusCreated, recorder.Code, "body = %s", recorder.Body.String())
	assert.Equal(t, tokenHash, gotHash, "session token hash")
	assert.Equal(t, "/api/v1/households/"+household.ID.String(), recorder.Header().Get("Location"), "Location")

	var body householdResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body), "decode response")
	assert.Equal(t, household.ID.String(), body.Household.ID, "response household = %+v", body.Household)
}

func TestHouseholdHandlerCreateMapsActivationFailure(t *testing.T) {
	handler := NewHouseholdHandler(stubHouseholdService{
		createAndActivate: func(context.Context, households.CreateParams) (households.Household, error) {
			return households.Household{}, auth.ErrSessionNotFound
		},
	}, discardLogger())

	recorder := sessionRequest(t,
		auth.Session{UserID: uuid.New(), User: auth.User{ID: uuid.New()}, TokenHash: strings.Repeat("a", 64)},
		http.HandlerFunc(handler.Create), http.MethodPost, "/api/v1/households", `{"name":"Home"}`)

	require.Equal(t, http.StatusUnauthorized, recorder.Code, "body = %s", recorder.Body.String())
	assert.Empty(t, recorder.Header().Get("Location"), "failed creation returned a Location header")
}

func TestHouseholdHandlerMapsServiceErrors(t *testing.T) {
	householdID := uuid.New()
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "validation", err: &households.ValidationError{Field: "name", Message: "Required."}, wantStatus: http.StatusUnprocessableEntity, wantCode: "validation_failed"},
		{name: "not found", err: households.ErrNotFound, wantStatus: http.StatusNotFound, wantCode: "not_found"},
		{name: "forbidden", err: households.ErrForbidden, wantStatus: http.StatusForbidden, wantCode: "forbidden"},
		{name: "internal", err: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError, wantCode: "internal_error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewHouseholdHandler(stubHouseholdService{
				get: func(context.Context, uuid.UUID, uuid.UUID) (households.Household, error) {
					return households.Household{}, tt.err
				},
			}, discardLogger())
			request := httptest.NewRequest(http.MethodGet, "/api/v1/households/"+householdID.String(), nil)
			request.SetPathValue("householdID", householdID.String())
			request.AddCookie(&http.Cookie{Name: auth.DefaultSessionCookieName, Value: "raw"})
			recorder := httptest.NewRecorder()

			userID := uuid.New()
			session := auth.Session{UserID: userID, User: auth.User{ID: userID}}
			auth.NewMiddleware(stubSessionLookup{session: session}, auth.CookieConfig{}, discardLogger()).
				Load(http.HandlerFunc(handler.Get)).ServeHTTP(recorder, request)

			require.Equal(t, tt.wantStatus, recorder.Code, "body = %s", recorder.Body.String())

			var body ErrorResponse
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body), "decode response")
			assert.Equal(t, tt.wantCode, body.Error.Code, "error code")
		})
	}
}

func TestHouseholdHandlerRejectsMalformedPathID(t *testing.T) {
	called := false
	handler := NewHouseholdHandler(stubHouseholdService{
		get: func(context.Context, uuid.UUID, uuid.UUID) (households.Household, error) {
			called = true
			return households.Household{}, nil
		},
	}, discardLogger())

	request := httptest.NewRequest(http.MethodGet, "/api/v1/households/not-a-uuid", nil)
	request.SetPathValue("householdID", "not-a-uuid")
	request.AddCookie(&http.Cookie{Name: auth.DefaultSessionCookieName, Value: "raw"})
	recorder := httptest.NewRecorder()
	userID := uuid.New()
	auth.NewMiddleware(
		stubSessionLookup{session: auth.Session{UserID: userID, User: auth.User{ID: userID}}},
		auth.CookieConfig{},
		discardLogger(),
	).Load(http.HandlerFunc(handler.Get)).ServeHTTP(recorder, request)

	assert.Equal(t, http.StatusNotFound, recorder.Code)
	assert.False(t, called, "service was called for a malformed household ID")
}
