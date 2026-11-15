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

	"budgettracker/internal/finance"
)

type stubRateService struct {
	list   func(context.Context, uuid.UUID, uuid.UUID) ([]finance.ExchangeRate, error)
	create func(context.Context, finance.CreateExchangeRateParams) (finance.ExchangeRate, error)
	update func(context.Context, finance.UpdateExchangeRateParams) (finance.ExchangeRate, error)
	delete func(context.Context, uuid.UUID, int64) error
}

func (s stubRateService) ListExchangeRates(ctx context.Context, actorID, householdID uuid.UUID) ([]finance.ExchangeRate, error) {
	return s.list(ctx, actorID, householdID)
}

func (s stubRateService) CreateExchangeRate(ctx context.Context, params finance.CreateExchangeRateParams) (finance.ExchangeRate, error) {
	return s.create(ctx, params)
}

func (s stubRateService) UpdateExchangeRate(ctx context.Context, params finance.UpdateExchangeRateParams) (finance.ExchangeRate, error) {
	return s.update(ctx, params)
}

func (s stubRateService) DeleteExchangeRate(ctx context.Context, actorID uuid.UUID, rateID int64) error {
	return s.delete(ctx, actorID, rateID)
}

func TestExchangeRateHandlerRejectsInvalidPathIDs(t *testing.T) {
	for _, raw := range []string{"abc", "0", "-1", "9223372036854775808"} {
		t.Run(raw, func(t *testing.T) {
			called := false
			handler := NewExchangeRateHandler(stubRateService{
				update: func(context.Context, finance.UpdateExchangeRateParams) (finance.ExchangeRate, error) {
					called = true
					return finance.ExchangeRate{}, nil
				},
			}, discardLogger())

			request := httptest.NewRequest(http.MethodPatch, "/api/v1/exchange-rates/"+raw, strings.NewReader(`{}`))
			request.SetPathValue("rateID", raw)
			recorder := serveEntryRequest(t, activeEntrySession(), handler.Update, request)

			assert.Equal(t, http.StatusNotFound, recorder.Code)
			assert.False(t, called, "service was called for an invalid rate ID")
		})
	}
}

func TestExchangeRateHandlerCreateResponse(t *testing.T) {
	session := activeEntrySession()
	rate := finance.ExchangeRate{
		ID:               5,
		HouseholdID:      *session.ActiveHouseholdID,
		FromCurrencyCode: "EUR",
		ToCurrencyCode:   "RSD",
		Rate:             "117.50000000",
	}
	handler := NewExchangeRateHandler(stubRateService{
		create: func(context.Context, finance.CreateExchangeRateParams) (finance.ExchangeRate, error) {
			return rate, nil
		},
	}, discardLogger())

	recorder := serveEntryRequest(t, session, handler.Create,
		httptest.NewRequest(http.MethodPost, "/api/v1/exchange-rates", strings.NewReader(`{"from_currency_code":"EUR","to_currency_code":"RSD","rate":"117.5","rate_date":"2026-10-01"}`)))

	require.Equal(t, http.StatusCreated, recorder.Code, "body = %s", recorder.Body.String())
	assert.Equal(t, "/api/v1/exchange-rates/5", recorder.Header().Get("Location"), "Location")

	var body exchangeRateResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body), "decode response")
	assert.Equal(t, "5", body.Rate.ID, "rate = %+v", body.Rate)
	assert.Equal(t, "117.5", body.Rate.Rate, "rate = %+v", body.Rate)
}

func TestExchangeRateHandlerMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "validation", err: &finance.ValidationError{Fields: map[string]string{"rate": "Required."}}, wantStatus: http.StatusUnprocessableEntity, wantCode: "validation_failed"},
		{name: "conflict", err: finance.ErrRateExists, wantStatus: http.StatusConflict, wantCode: "rate_exists"},
		{name: "not found", err: finance.ErrNotFound, wantStatus: http.StatusNotFound, wantCode: "not_found"},
		{name: "forbidden", err: finance.ErrForbidden, wantStatus: http.StatusForbidden, wantCode: "forbidden"},
		{name: "internal", err: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError, wantCode: "internal_error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewExchangeRateHandler(stubRateService{
				list: func(context.Context, uuid.UUID, uuid.UUID) ([]finance.ExchangeRate, error) {
					return nil, tt.err
				},
			}, discardLogger())
			recorder := serveEntryRequest(t, activeEntrySession(), handler.List,
				httptest.NewRequest(http.MethodGet, "/api/v1/exchange-rates", nil))

			require.Equal(t, tt.wantStatus, recorder.Code, "body = %s", recorder.Body.String())

			var body ErrorResponse
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body), "decode response")
			assert.Equal(t, tt.wantCode, body.Error.Code, "error code")
		})
	}
}
