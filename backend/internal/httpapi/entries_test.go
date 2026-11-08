package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"budgettracker/internal/auth"
	"budgettracker/internal/finance"
)

type stubEntryService struct {
	currencies func(context.Context) ([]finance.Currency, error)
	create     func(context.Context, finance.CreateParams) (finance.Entry, error)
	get        func(context.Context, uuid.UUID, finance.Kind, int64) (finance.Entry, error)
	update     func(context.Context, finance.UpdateParams) (finance.Entry, error)
	delete     func(context.Context, uuid.UUID, finance.Kind, int64) error
	list       func(context.Context, finance.ListParams) (finance.ListResult, error)
}

func (s stubEntryService) Currencies(ctx context.Context) ([]finance.Currency, error) {
	return s.currencies(ctx)
}

func (s stubEntryService) Create(ctx context.Context, params finance.CreateParams) (finance.Entry, error) {
	return s.create(ctx, params)
}

func (s stubEntryService) Get(ctx context.Context, actorID uuid.UUID, kind finance.Kind, id int64) (finance.Entry, error) {
	return s.get(ctx, actorID, kind, id)
}

func (s stubEntryService) Update(ctx context.Context, params finance.UpdateParams) (finance.Entry, error) {
	return s.update(ctx, params)
}

func (s stubEntryService) Delete(ctx context.Context, actorID uuid.UUID, kind finance.Kind, id int64) error {
	return s.delete(ctx, actorID, kind, id)
}

func (s stubEntryService) List(ctx context.Context, params finance.ListParams) (finance.ListResult, error) {
	return s.list(ctx, params)
}

func serveEntryRequest(t *testing.T, session auth.Session, handler http.HandlerFunc, request *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	request.AddCookie(&http.Cookie{Name: auth.DefaultSessionCookieName, Value: "raw"})
	if request.ContentLength != 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	auth.NewMiddleware(stubSessionLookup{session: session}, auth.CookieConfig{}, discardLogger()).
		Load(handler).ServeHTTP(recorder, request)
	return recorder
}

func activeEntrySession() auth.Session {
	userID := uuid.New()
	householdID := uuid.New()
	return auth.Session{
		UserID:            userID,
		User:              auth.User{ID: userID},
		ActiveHouseholdID: &householdID,
	}
}

func TestEntryHandlerRequiresActiveHousehold(t *testing.T) {
	handler := NewEntryHandler(stubEntryService{
		list: func(context.Context, finance.ListParams) (finance.ListResult, error) {
			t.Error("service was called without an active household")
			return finance.ListResult{}, nil
		},
	}, discardLogger())

	userID := uuid.New()
	session := auth.Session{UserID: userID, User: auth.User{ID: userID}}
	recorder := serveEntryRequest(t, session, handler.ListIncome,
		httptest.NewRequest(http.MethodGet, "/api/v1/incomes", nil))

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnprocessableEntity)
	}
	var body ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error.Fields["household_id"] == "" {
		t.Errorf("fields = %v, want a household_id error", body.Error.Fields)
	}
}

func TestEntryHandlerRejectsInvalidPathIDs(t *testing.T) {
	for _, raw := range []string{"abc", "0", "-1", "9223372036854775808"} {
		t.Run(raw, func(t *testing.T) {
			called := false
			handler := NewEntryHandler(stubEntryService{
				get: func(context.Context, uuid.UUID, finance.Kind, int64) (finance.Entry, error) {
					called = true
					return finance.Entry{}, nil
				},
			}, discardLogger())

			request := httptest.NewRequest(http.MethodGet, "/api/v1/incomes/"+raw, nil)
			request.SetPathValue("entryID", raw)
			recorder := serveEntryRequest(t, activeEntrySession(), handler.GetIncome, request)

			if recorder.Code != http.StatusNotFound {
				t.Errorf("status = %d, want %d", recorder.Code, http.StatusNotFound)
			}
			if called {
				t.Error("service was called for an invalid entry ID")
			}
		})
	}
}

func TestEntryHandlerParsesQuery(t *testing.T) {
	var got finance.ListParams
	handler := NewEntryHandler(stubEntryService{
		list: func(_ context.Context, params finance.ListParams) (finance.ListResult, error) {
			got = params
			return finance.ListResult{Page: params.Page, PageSize: params.PageSize}, nil
		},
	}, discardLogger())

	recorder := serveEntryRequest(t, activeEntrySession(), handler.ListIncome,
		httptest.NewRequest(http.MethodGet, "/api/v1/incomes?scope=all&from=2026-10-01&to=2026-10-31&page=2&page_size=5", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got.Scope != finance.ScopeAll || got.Page != 2 || got.PageSize != 5 {
		t.Errorf("params = %+v", got)
	}
	if got.From == nil || got.From.Format("2006-01-02") != "2026-10-01" {
		t.Errorf("from = %v", got.From)
	}
	if got.To == nil || got.To.Format("2006-01-02") != "2026-10-31" {
		t.Errorf("to = %v", got.To)
	}
}

func TestEntryHandlerRejectsInvalidQuery(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{name: "scope", query: "scope=everyone"},
		{name: "from date", query: "from=yesterday"},
		{name: "to date", query: "to=2026-13-01"},
		{name: "page", query: "page=many"},
		{name: "page size", query: "page_size=lots"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewEntryHandler(stubEntryService{
				list: func(context.Context, finance.ListParams) (finance.ListResult, error) {
					t.Error("service was called for an invalid query")
					return finance.ListResult{}, nil
				},
			}, discardLogger())
			recorder := serveEntryRequest(t, activeEntrySession(), handler.ListIncome,
				httptest.NewRequest(http.MethodGet, "/api/v1/incomes?"+tt.query, nil))
			if recorder.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestEntryHandlerMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "validation", err: &finance.ValidationError{Fields: map[string]string{"amount": "Required."}}, wantStatus: http.StatusUnprocessableEntity, wantCode: "validation_failed"},
		{name: "not found", err: finance.ErrNotFound, wantStatus: http.StatusNotFound, wantCode: "not_found"},
		{name: "forbidden", err: finance.ErrForbidden, wantStatus: http.StatusForbidden, wantCode: "forbidden"},
		{name: "internal", err: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError, wantCode: "internal_error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewEntryHandler(stubEntryService{
				get: func(context.Context, uuid.UUID, finance.Kind, int64) (finance.Entry, error) {
					return finance.Entry{}, tt.err
				},
			}, discardLogger())
			request := httptest.NewRequest(http.MethodGet, "/api/v1/incomes/42", nil)
			request.SetPathValue("entryID", "42")
			recorder := serveEntryRequest(t, activeEntrySession(), handler.GetIncome, request)

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

func TestEntryHandlerCreateResponse(t *testing.T) {
	session := activeEntrySession()
	householdID := *session.ActiveHouseholdID
	entry := finance.Entry{
		ID:                 42,
		Kind:               finance.KindIncome,
		HouseholdID:        householdID,
		AmountMinor:        1234,
		CurrencyCode:       "RSD",
		CurrencyMinorUnits: 2,
		EntryDate:          time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		CreatedAt:          time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC),
		UpdatedAt:          time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC),
	}
	var got finance.CreateParams
	handler := NewEntryHandler(stubEntryService{
		create: func(_ context.Context, params finance.CreateParams) (finance.Entry, error) {
			got = params
			return entry, nil
		},
	}, discardLogger())

	recorder := serveEntryRequest(t, session, handler.CreateIncome,
		httptest.NewRequest(http.MethodPost, "/api/v1/incomes", strings.NewReader(`{"amount":"12.34"}`)))

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if got.HouseholdID != householdID || got.ActorID != session.User.ID {
		t.Errorf("params = %+v", got)
	}
	if got := recorder.Header().Get("Location"); got != "/api/v1/incomes/42" {
		t.Errorf("Location = %q", got)
	}
	var body entryResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Entry.ID != "42" || body.Entry.Amount != "12.34" {
		t.Errorf("entry = %+v", body.Entry)
	}
	if body.Entry.UserID != nil || body.Entry.Description != nil {
		t.Errorf("nullable fields = user_id %v, description %v, want null", body.Entry.UserID, body.Entry.Description)
	}
}

func TestEntryHandlerListEnvelope(t *testing.T) {
	householdID := uuid.New()
	handler := NewEntryHandler(stubEntryService{
		list: func(context.Context, finance.ListParams) (finance.ListResult, error) {
			return finance.ListResult{
				Entries: []finance.Entry{{
					ID:                 7,
					Kind:               finance.KindSpending,
					HouseholdID:        householdID,
					AmountMinor:        500,
					CurrencyCode:       "RSD",
					CurrencyMinorUnits: 2,
					EntryDate:          time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC),
				}},
				Page:       1,
				PageSize:   20,
				Total:      1,
				TotalPages: 1,
			}, nil
		},
	}, discardLogger())

	recorder := serveEntryRequest(t, activeEntrySession(), handler.ListSpending,
		httptest.NewRequest(http.MethodGet, "/api/v1/spendings", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var body entriesResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Entries) != 1 || body.Entries[0].ID != "7" {
		t.Errorf("entries = %+v", body.Entries)
	}
	if body.Pagination.Total != 1 || body.Pagination.TotalPages != 1 {
		t.Errorf("pagination = %+v", body.Pagination)
	}
}
