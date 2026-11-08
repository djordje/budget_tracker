//go:build integration

package server

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"budgettracker/internal/testdb"
)

type rateEnvelope struct {
	Rate struct {
		ID               string `json:"id"`
		HouseholdID      string `json:"household_id"`
		FromCurrencyCode string `json:"from_currency_code"`
		ToCurrencyCode   string `json:"to_currency_code"`
		Rate             string `json:"rate"`
		RateDate         string `json:"rate_date"`
		IsMonthly        bool   `json:"is_monthly"`
	} `json:"rate"`
}

type ratesEnvelope struct {
	Rates []struct {
		ID               string `json:"id"`
		FromCurrencyCode string `json:"from_currency_code"`
		ToCurrencyCode   string `json:"to_currency_code"`
		Rate             string `json:"rate"`
		RateDate         string `json:"rate_date"`
		IsMonthly        bool   `json:"is_monthly"`
	} `json:"rates"`
}

type summaryEnvelope struct {
	Summary struct {
		From               string `json:"from"`
		To                 string `json:"to"`
		Scope              string `json:"scope"`
		DisplayCurrency    string `json:"display_currency"`
		IncomeMinor        int64  `json:"income_minor"`
		SpendingMinor      int64  `json:"spending_minor"`
		NetMinor           int64  `json:"net_minor"`
		Income             string `json:"income"`
		Spending           string `json:"spending"`
		Net                string `json:"net"`
		EntryCount         int    `json:"entry_count"`
		IncludedEntryCount int    `json:"included_entry_count"`
		MissingEntryCount  int    `json:"missing_entry_count"`
		Complete           bool   `json:"complete"`
		MissingRates       []struct {
			FromCurrencyCode string `json:"from_currency_code"`
			ToCurrencyCode   string `json:"to_currency_code"`
			EntryDate        string `json:"entry_date"`
		} `json:"missing_rates"`
	} `json:"summary"`
}

func createRateAPI(t *testing.T, client *testClient, csrf string, body map[string]any) rateEnvelope {
	t.Helper()
	response := client.apiRequest(t, http.MethodPost, "/api/v1/exchange-rates", body, csrf)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create rate = %d, body:\n%s", response.StatusCode, readBody(t, response))
	}
	var created rateEnvelope
	decodeBody(t, response, &created)
	return created
}

func TestExchangeRateAPI(t *testing.T) {
	pool := testdb.New(t)
	admin := newTestClientWithPool(t, pool)
	outsider := newTestClientWithPool(t, pool)
	adminCSRF := admin.registerHTML(t, "admin@example.com")
	createAPIHousehold(t, admin, adminCSRF, "Home")

	response := admin.apiRequest(t, http.MethodGet, "/api/v1/exchange-rates", nil, "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("list rates = %d", response.StatusCode)
	}
	var empty ratesEnvelope
	decodeBody(t, response, &empty)
	if len(empty.Rates) != 0 {
		t.Errorf("initial rates = %+v", empty.Rates)
	}

	exact := createRateAPI(t, admin, adminCSRF, map[string]any{
		"from_currency_code": "eur",
		"to_currency_code":   "rsd",
		"rate":               "117.25",
		"rate_date":          "2026-10-15",
	})
	if exact.Rate.FromCurrencyCode != "EUR" || exact.Rate.ToCurrencyCode != "RSD" || exact.Rate.IsMonthly {
		t.Errorf("exact rate = %+v", exact.Rate)
	}

	monthly := createRateAPI(t, admin, adminCSRF, map[string]any{
		"from_currency_code": "USD",
		"to_currency_code":   "RSD",
		"rate":               "100",
		"rate_date":          "2026-10-22",
		"is_monthly":         true,
	})
	if !monthly.Rate.IsMonthly || monthly.Rate.RateDate != "2026-10-01" {
		t.Errorf("monthly rate = %+v", monthly.Rate)
	}

	response = admin.apiRequest(t, http.MethodPost, "/api/v1/exchange-rates", map[string]any{
		"from_currency_code": "EUR",
		"to_currency_code":   "RSD",
		"rate":               "118",
		"rate_date":          "2026-10-15",
	}, adminCSRF)
	if response.StatusCode != http.StatusConflict {
		t.Errorf("duplicate rate = %d, want 409", response.StatusCode)
	}
	var conflict errorEnvelope
	decodeBody(t, response, &conflict)
	if conflict.Error.Code != "rate_exists" {
		t.Errorf("conflict code = %q", conflict.Error.Code)
	}

	response = admin.apiRequest(t, http.MethodGet, "/api/v1/exchange-rates", nil, "")
	var listed ratesEnvelope
	decodeBody(t, response, &listed)
	if len(listed.Rates) != 2 {
		t.Errorf("rate count = %d, want 2", len(listed.Rates))
	}

	rateID := exact.Rate.ID
	response = admin.apiRequest(t, http.MethodPatch, "/api/v1/exchange-rates/"+rateID, map[string]any{
		"rate": "120.50",
	}, adminCSRF)
	var updated rateEnvelope
	decodeBody(t, response, &updated)
	if response.StatusCode != http.StatusOK || updated.Rate.Rate != "120.5" {
		t.Errorf("update rate = %d, %q", response.StatusCode, updated.Rate.Rate)
	}

	response = admin.apiRequest(t, http.MethodPost, "/api/v1/exchange-rates", map[string]any{
		"from_currency_code": "EUR",
		"to_currency_code":   "EUR",
		"rate":               "1",
		"rate_date":          "2026-10-15",
	}, adminCSRF)
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("same currency rate = %d, want 422", response.StatusCode)
	}
	response.Body.Close()

	outsiderCSRF := outsider.registerHTML(t, "outsider@example.com")
	response = outsider.apiRequest(t, http.MethodPatch, "/api/v1/exchange-rates/"+rateID, map[string]any{
		"rate": "1",
	}, outsiderCSRF)
	if response.StatusCode != http.StatusNotFound {
		t.Errorf("outsider update = %d, want 404", response.StatusCode)
	}
	response.Body.Close()

	response = admin.apiRequest(t, http.MethodDelete, "/api/v1/exchange-rates/"+rateID, nil, adminCSRF)
	if response.StatusCode != http.StatusNoContent {
		t.Errorf("delete rate = %d, want 204", response.StatusCode)
	}
	response.Body.Close()

	response = admin.apiRequest(t, http.MethodPost, "/api/v1/exchange-rates", map[string]any{
		"from_currency_code": "EUR",
		"to_currency_code":   "RSD",
		"rate":               "1",
		"rate_date":          "2026-10-15",
	}, "")
	if response.StatusCode != http.StatusForbidden {
		t.Errorf("rate without CSRF = %d, want 403", response.StatusCode)
	}
	response.Body.Close()
}

func TestSummaryAPI(t *testing.T) {
	pool := testdb.New(t)
	admin := newTestClientWithPool(t, pool)
	member := newTestClientWithPool(t, pool)
	adminCSRF := admin.registerHTML(t, "admin@example.com")
	memberCSRF := member.registerHTML(t, "member@example.com")
	homeID := createAPIHousehold(t, admin, adminCSRF, "Home")

	memberID := userIDByEmail(t, pool, "member@example.com")
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO household_memberships (household_id, user_id, role)
		VALUES ($1, $2, 'member')`, homeID, memberID,
	); err != nil {
		t.Fatalf("add member: %v", err)
	}
	response := member.postForm(t, "/app/active-household", url.Values{
		"csrf_token":   {memberCSRF},
		"household_id": {homeID.String()},
	})
	readBody(t, response)

	createRateAPI(t, admin, adminCSRF, map[string]any{
		"from_currency_code": "EUR", "to_currency_code": "RSD",
		"rate": "117.5", "rate_date": "2026-10-15",
	})
	createRateAPI(t, admin, adminCSRF, map[string]any{
		"from_currency_code": "USD", "to_currency_code": "RSD",
		"rate": "100", "rate_date": "2026-10-01", "is_monthly": true,
	})

	createEntryAPI(t, admin, adminCSRF, "/api/v1/incomes", map[string]any{
		"amount": "100", "entry_date": "2026-10-15",
	})
	createEntryAPI(t, admin, adminCSRF, "/api/v1/spendings", map[string]any{
		"amount": "10", "currency_code": "EUR", "entry_date": "2026-10-15",
	})
	createEntryAPI(t, member, memberCSRF, "/api/v1/incomes", map[string]any{
		"amount": "20", "currency_code": "USD", "entry_date": "2026-10-15",
	})

	response = admin.apiRequest(t, http.MethodGet, "/api/v1/reports/summary?scope=all&from=2026-10-01&to=2026-10-31&currency=RSD", nil, "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("summary = %d, body:\n%s", response.StatusCode, readBody(t, response))
	}
	var summary summaryEnvelope
	decodeBody(t, response, &summary)
	if summary.Summary.IncomeMinor != 210000 || summary.Summary.SpendingMinor != 117500 || summary.Summary.NetMinor != 92500 {
		t.Errorf("summary totals = %+v", summary.Summary)
	}
	if !summary.Summary.Complete || summary.Summary.EntryCount != 3 {
		t.Errorf("summary completeness = %+v", summary.Summary)
	}
	if summary.Summary.Income != "2100.00" || summary.Summary.Net != "925.00" {
		t.Errorf("formatted summary = %+v", summary.Summary)
	}

	response = admin.apiRequest(t, http.MethodGet, "/api/v1/reports/summary?scope=mine&from=2026-10-01&to=2026-10-31&currency=RSD", nil, "")
	var mine summaryEnvelope
	decodeBody(t, response, &mine)
	if mine.Summary.IncomeMinor != 10000 || mine.Summary.SpendingMinor != 117500 {
		t.Errorf("admin mine = %+v", mine.Summary)
	}

	response = member.apiRequest(t, http.MethodGet, "/api/v1/reports/summary?scope=all&from=2026-10-01&to=2026-10-31", nil, "")
	if response.StatusCode != http.StatusForbidden {
		t.Errorf("member all summary = %d, want 403", response.StatusCode)
	}
	response.Body.Close()

	createEntryAPI(t, admin, adminCSRF, "/api/v1/spendings", map[string]any{
		"amount": "5", "currency_code": "GBP", "entry_date": "2026-10-15",
	})
	response = admin.apiRequest(t, http.MethodGet, "/api/v1/reports/summary?scope=all&from=2026-10-01&to=2026-10-31&currency=RSD", nil, "")
	var incomplete summaryEnvelope
	decodeBody(t, response, &incomplete)
	if incomplete.Summary.Complete || incomplete.Summary.MissingEntryCount != 1 || len(incomplete.Summary.MissingRates) != 1 {
		t.Errorf("incomplete summary = %+v", incomplete.Summary)
	}
	if incomplete.Summary.MissingRates[0].FromCurrencyCode != "GBP" ||
		incomplete.Summary.MissingRates[0].EntryDate != "2026-10-15" {
		t.Errorf("missing rate = %+v", incomplete.Summary.MissingRates)
	}

	response = admin.apiRequest(t, http.MethodGet, "/api/v1/reports/summary?household_id="+homeID.String()+"&scope=all&from=2026-10-01&to=2026-10-31", nil, "")
	if response.StatusCode != http.StatusOK {
		t.Errorf("explicit household summary = %d", response.StatusCode)
	}
	response.Body.Close()

	response = admin.apiRequest(t, http.MethodGet, "/api/v1/reports/summary?scope=all", nil, "")
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("missing summary dates = %d, want 422", response.StatusCode)
	}
	response.Body.Close()

	response = admin.apiRequest(t, http.MethodGet, "/api/v1/reports/summary?scope=team&from=2026-10-01&to=2026-10-31", nil, "")
	if response.StatusCode != http.StatusBadRequest {
		t.Errorf("invalid summary scope = %d, want 400", response.StatusCode)
	}
	response.Body.Close()

	noHousehold := newTestClientWithPool(t, pool)
	noHousehold.registerHTML(t, "no-household@example.com")
	response = noHousehold.apiRequest(t, http.MethodGet, "/api/v1/reports/summary?from=2026-10-01&to=2026-10-31", nil, "")
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("summary without household = %d, want 422", response.StatusCode)
	}
	response.Body.Close()
}
