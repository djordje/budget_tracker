package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"budgettracker/internal/auth"
	"budgettracker/internal/households"
)

type stubHouseholdService struct {
	HouseholdService
	createAndActivate func(context.Context, uuid.UUID, string, string) (households.Household, error)
	list              func(context.Context, uuid.UUID) ([]households.Household, error)
	get               func(context.Context, uuid.UUID, uuid.UUID) (households.Household, error)
	members           func(context.Context, uuid.UUID, uuid.UUID) ([]households.Member, error)
	getOptions        func(context.Context, uuid.UUID, uuid.UUID) (households.Options, error)
	getOptionsView    func(context.Context, uuid.UUID, uuid.UUID) (households.OptionsView, error)
	setUserOptions    func(context.Context, uuid.UUID, uuid.UUID, households.UpdateUserOptionsParams) (households.OptionsView, error)
	revokeInvitation  func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error
}

func (s stubHouseholdService) CreateAndActivate(
	ctx context.Context, userID uuid.UUID, sessionTokenHash, name string,
) (households.Household, error) {
	return s.createAndActivate(ctx, userID, sessionTokenHash, name)
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

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	var body ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error.Code != "unauthenticated" {
		t.Errorf("error code = %q", body.Error.Code)
	}
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
		createAndActivate: func(_ context.Context, actorID uuid.UUID, sessionHash, name string) (households.Household, error) {
			if actorID != userID || name != "Home" {
				t.Errorf("CreateAndActivate(%s, %q), want user %s and Home", actorID, name, userID)
			}
			gotHash = sessionHash
			return household, nil
		},
	}, discardLogger())

	recorder := sessionRequest(t, auth.Session{UserID: userID, User: auth.User{ID: userID}, TokenHash: tokenHash}, http.HandlerFunc(handler.Create),
		http.MethodPost, "/api/v1/households", `{"name":"Home"}`)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if gotHash != tokenHash {
		t.Errorf("session token hash = %q, want %q", gotHash, tokenHash)
	}
	if got := recorder.Header().Get("Location"); got != "/api/v1/households/"+household.ID.String() {
		t.Errorf("Location = %q", got)
	}
	var body householdResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Household.ID != household.ID.String() {
		t.Errorf("response household = %+v", body.Household)
	}
}

func TestHouseholdHandlerCreateMapsActivationFailure(t *testing.T) {
	handler := NewHouseholdHandler(stubHouseholdService{
		createAndActivate: func(context.Context, uuid.UUID, string, string) (households.Household, error) {
			return households.Household{}, auth.ErrSessionNotFound
		},
	}, discardLogger())

	recorder := sessionRequest(t,
		auth.Session{UserID: uuid.New(), User: auth.User{ID: uuid.New()}, TokenHash: strings.Repeat("a", 64)},
		http.HandlerFunc(handler.Create), http.MethodPost, "/api/v1/households", `{"name":"Home"}`)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if recorder.Header().Get("Location") != "" {
		t.Error("failed creation returned a Location header")
	}
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

			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.wantStatus)
			}
			var body ErrorResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if body.Error.Code != tt.wantCode {
				t.Errorf("error code = %q, want %q", body.Error.Code, tt.wantCode)
			}
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

	if recorder.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if called {
		t.Error("service was called for a malformed household ID")
	}
}
