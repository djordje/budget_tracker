package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"budgettracker/internal/finance"
	"budgettracker/internal/reporting"
)

type stubReportService struct {
	summary func(context.Context, reporting.SummaryParams) (reporting.Summary, error)
}

func (s stubReportService) Summary(ctx context.Context, params reporting.SummaryParams) (reporting.Summary, error) {
	return s.summary(ctx, params)
}

func TestReportHandlerTranslatesQuery(t *testing.T) {
	session := activeEntrySession()
	householdID := *session.ActiveHouseholdID
	var got reporting.SummaryParams
	handler := NewReportHandler(stubReportService{
		summary: func(_ context.Context, params reporting.SummaryParams) (reporting.Summary, error) {
			got = params
			return reporting.Summary{Scope: params.Scope, DisplayCurrency: "EUR"}, nil
		},
	}, discardLogger())

	recorder := serveEntryRequest(t, session, handler.Summary, httptest.NewRequest(
		http.MethodGet,
		"/api/v1/reports/summary?from=2026-10-01&to=2026-10-31&scope=all&currency=EUR&household_id="+householdID.String(),
		nil,
	))

	require.Equal(t, http.StatusOK, recorder.Code, "body = %s", recorder.Body.String())
	assert.Equal(t, householdID, got.HouseholdID, "params = %+v", got)
	assert.Equal(t, finance.ScopeAll, got.Scope, "params = %+v", got)
	assert.Equal(t, "EUR", got.DisplayCurrency, "params = %+v", got)
	assert.Equal(t, "2026-10-01", got.From.Format("2006-01-02"), "from")
	assert.Equal(t, "2026-10-31", got.To.Format("2006-01-02"), "to")
}

func TestReportHandlerRejectsInvalidRequests(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		wantStatus int
	}{
		{name: "missing from", query: "to=2026-10-31", wantStatus: http.StatusUnprocessableEntity},
		{name: "missing to", query: "from=2026-10-01", wantStatus: http.StatusUnprocessableEntity},
		{name: "invalid from", query: "from=yesterday&to=2026-10-31", wantStatus: http.StatusUnprocessableEntity},
		{name: "padded from", query: "from=%202026-10-01&to=2026-10-31", wantStatus: http.StatusUnprocessableEntity},
		{name: "whitespace-only from", query: "from=+&to=2026-10-31", wantStatus: http.StatusUnprocessableEntity},
		{name: "invalid scope", query: "from=2026-10-01&to=2026-10-31&scope=everyone", wantStatus: http.StatusBadRequest},
		{name: "invalid household", query: "from=2026-10-01&to=2026-10-31&household_id=nope", wantStatus: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewReportHandler(stubReportService{
				summary: func(context.Context, reporting.SummaryParams) (reporting.Summary, error) {
					assert.Fail(t, "service was called for an invalid request")
					return reporting.Summary{}, nil
				},
			}, discardLogger())
			recorder := serveEntryRequest(t, activeEntrySession(), handler.Summary,
				httptest.NewRequest(http.MethodGet, "/api/v1/reports/summary?"+tt.query, nil))
			assert.Equal(t, tt.wantStatus, recorder.Code)
		})
	}
}

func TestReportHandlerMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "validation", err: &finance.ValidationError{Fields: map[string]string{"currency": "Required."}}, wantStatus: http.StatusUnprocessableEntity, wantCode: "validation_failed"},
		{name: "forbidden", err: finance.ErrForbidden, wantStatus: http.StatusForbidden, wantCode: "forbidden"},
		{name: "not found", err: finance.ErrNotFound, wantStatus: http.StatusNotFound, wantCode: "not_found"},
		{name: "internal", err: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError, wantCode: "internal_error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewReportHandler(stubReportService{
				summary: func(context.Context, reporting.SummaryParams) (reporting.Summary, error) {
					return reporting.Summary{}, tt.err
				},
			}, discardLogger())
			recorder := serveEntryRequest(t, activeEntrySession(), handler.Summary,
				httptest.NewRequest(http.MethodGet, "/api/v1/reports/summary?from=2026-10-01&to=2026-10-31", nil))

			require.Equal(t, tt.wantStatus, recorder.Code, "body = %s", recorder.Body.String())

			var body ErrorResponse
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body), "decode response")
			assert.Equal(t, tt.wantCode, body.Error.Code, "error code")
		})
	}
}

func TestReportHandlerMissingRateData(t *testing.T) {
	handler := NewReportHandler(stubReportService{
		summary: func(context.Context, reporting.SummaryParams) (reporting.Summary, error) {
			return reporting.Summary{
				From:              "2026-10-01",
				To:                "2026-10-31",
				Scope:             finance.ScopeMine,
				DisplayCurrency:   "RSD",
				DisplayMinorUnits: 2,
				EntryCount:        1,
				MissingEntryCount: 1,
				Complete:          false,
				MissingRates: []reporting.MissingRate{{
					FromCurrencyCode: "GBP",
					ToCurrencyCode:   "RSD",
					EntryDate:        "2026-10-15",
				}},
			}, nil
		},
	}, discardLogger())

	session := activeEntrySession()
	recorder := serveEntryRequest(t, session, handler.Summary,
		httptest.NewRequest(http.MethodGet, "/api/v1/reports/summary?from=2026-10-01&to=2026-10-31", nil))

	require.Equal(t, http.StatusOK, recorder.Code, "body = %s", recorder.Body.String())

	var body summaryResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body), "decode response")
	assert.False(t, body.Summary.Complete, "summary reported complete with a missing rate")
	require.Len(t, body.Summary.MissingRates, 1, "missing rates = %+v", body.Summary.MissingRates)
	assert.Equal(t, "GBP", body.Summary.MissingRates[0].FromCurrencyCode, "missing rates = %+v", body.Summary.MissingRates)
}
