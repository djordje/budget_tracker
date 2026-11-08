package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

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

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if got.HouseholdID != householdID || got.Scope != finance.ScopeAll || got.DisplayCurrency != "EUR" {
		t.Errorf("params = %+v", got)
	}
	if got.From.Format("2006-01-02") != "2026-10-01" || got.To.Format("2006-01-02") != "2026-10-31" {
		t.Errorf("date range = %v to %v", got.From, got.To)
	}
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
		{name: "invalid scope", query: "from=2026-10-01&to=2026-10-31&scope=everyone", wantStatus: http.StatusBadRequest},
		{name: "invalid household", query: "from=2026-10-01&to=2026-10-31&household_id=nope", wantStatus: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewReportHandler(stubReportService{
				summary: func(context.Context, reporting.SummaryParams) (reporting.Summary, error) {
					t.Error("service was called for an invalid request")
					return reporting.Summary{}, nil
				},
			}, discardLogger())
			recorder := serveEntryRequest(t, activeEntrySession(), handler.Summary,
				httptest.NewRequest(http.MethodGet, "/api/v1/reports/summary?"+tt.query, nil))
			if recorder.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", recorder.Code, tt.wantStatus)
			}
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

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var body summaryResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Summary.Complete {
		t.Error("summary reported complete with a missing rate")
	}
	if len(body.Summary.MissingRates) != 1 || body.Summary.MissingRates[0].FromCurrencyCode != "GBP" {
		t.Errorf("missing rates = %+v", body.Summary.MissingRates)
	}
}
