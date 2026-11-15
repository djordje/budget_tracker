//go:build integration

package server

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	if !assert.Equal(t, http.StatusCreated, response.StatusCode, "create rate") {
		t.Fatalf("body:\n%s", readBody(t, response))
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
	require.Equal(t, http.StatusOK, response.StatusCode, "list rates")
	var empty ratesEnvelope
	decodeBody(t, response, &empty)
	assert.Empty(t, empty.Rates, "initial rates")

	exact := createRateAPI(t, admin, adminCSRF, map[string]any{
		"from_currency_code": "eur",
		"to_currency_code":   "rsd",
		"rate":               "117.25",
		"rate_date":          "2026-10-15",
	})
	assert.Equal(t, "EUR", exact.Rate.FromCurrencyCode, "exact rate = %+v", exact.Rate)
	assert.Equal(t, "RSD", exact.Rate.ToCurrencyCode, "exact rate = %+v", exact.Rate)
	assert.False(t, exact.Rate.IsMonthly, "exact rate = %+v", exact.Rate)

	monthly := createRateAPI(t, admin, adminCSRF, map[string]any{
		"from_currency_code": "USD",
		"to_currency_code":   "RSD",
		"rate":               "100",
		"rate_date":          "2026-10-22",
		"is_monthly":         true,
	})
	assert.True(t, monthly.Rate.IsMonthly, "monthly rate = %+v", monthly.Rate)
	assert.Equal(t, "2026-10-01", monthly.Rate.RateDate, "monthly rate = %+v", monthly.Rate)

	response = admin.apiRequest(t, http.MethodPost, "/api/v1/exchange-rates", map[string]any{
		"from_currency_code": "EUR",
		"to_currency_code":   "RSD",
		"rate":               "118",
		"rate_date":          "2026-10-15",
	}, adminCSRF)
	assert.Equal(t, http.StatusConflict, response.StatusCode, "duplicate rate")
	var conflict errorEnvelope
	decodeBody(t, response, &conflict)
	assert.Equal(t, "rate_exists", conflict.Error.Code, "conflict code")

	response = admin.apiRequest(t, http.MethodGet, "/api/v1/exchange-rates", nil, "")
	var listed ratesEnvelope
	decodeBody(t, response, &listed)
	assert.Len(t, listed.Rates, 2, "rate count")

	rateID := exact.Rate.ID
	response = admin.apiRequest(t, http.MethodPatch, "/api/v1/exchange-rates/"+rateID, map[string]any{
		"rate": "120.50",
	}, adminCSRF)
	var updated rateEnvelope
	decodeBody(t, response, &updated)
	assert.Equal(t, http.StatusOK, response.StatusCode, "update rate = %+v", updated.Rate)
	assert.Equal(t, "120.5", updated.Rate.Rate, "update rate")

	response = admin.apiRequest(t, http.MethodPost, "/api/v1/exchange-rates", map[string]any{
		"from_currency_code": "EUR",
		"to_currency_code":   "EUR",
		"rate":               "1",
		"rate_date":          "2026-10-15",
	}, adminCSRF)
	assert.Equal(t, http.StatusUnprocessableEntity, response.StatusCode, "same currency rate")
	response.Body.Close()

	outsiderCSRF := outsider.registerHTML(t, "outsider@example.com")
	response = outsider.apiRequest(t, http.MethodPatch, "/api/v1/exchange-rates/"+rateID, map[string]any{
		"rate": "1",
	}, outsiderCSRF)
	assert.Equal(t, http.StatusNotFound, response.StatusCode, "outsider update")
	response.Body.Close()

	response = admin.apiRequest(t, http.MethodDelete, "/api/v1/exchange-rates/"+rateID, nil, adminCSRF)
	assert.Equal(t, http.StatusNoContent, response.StatusCode, "delete rate")
	response.Body.Close()

	response = admin.apiRequest(t, http.MethodPost, "/api/v1/exchange-rates", map[string]any{
		"from_currency_code": "EUR",
		"to_currency_code":   "RSD",
		"rate":               "1",
		"rate_date":          "2026-10-15",
	}, "")
	assert.Equal(t, http.StatusForbidden, response.StatusCode, "rate without CSRF")
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
		require.NoError(t, err, "add member")
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
	if !assert.Equal(t, http.StatusOK, response.StatusCode, "summary") {
		t.Fatalf("body:\n%s", readBody(t, response))
	}
	var summary summaryEnvelope
	decodeBody(t, response, &summary)
	assert.Equal(t, int64(210000), summary.Summary.IncomeMinor, "summary = %+v", summary.Summary)
	assert.Equal(t, int64(117500), summary.Summary.SpendingMinor, "summary = %+v", summary.Summary)
	assert.Equal(t, int64(92500), summary.Summary.NetMinor, "summary = %+v", summary.Summary)
	assert.True(t, summary.Summary.Complete, "summary = %+v", summary.Summary)
	assert.Equal(t, 3, summary.Summary.EntryCount, "summary = %+v", summary.Summary)
	assert.Equal(t, "2100.00", summary.Summary.Income, "summary = %+v", summary.Summary)
	assert.Equal(t, "925.00", summary.Summary.Net, "summary = %+v", summary.Summary)

	response = admin.apiRequest(t, http.MethodGet, "/api/v1/reports/summary?scope=mine&from=2026-10-01&to=2026-10-31&currency=RSD", nil, "")
	var mine summaryEnvelope
	decodeBody(t, response, &mine)
	assert.Equal(t, int64(10000), mine.Summary.IncomeMinor, "admin mine = %+v", mine.Summary)
	assert.Equal(t, int64(117500), mine.Summary.SpendingMinor, "admin mine = %+v", mine.Summary)

	response = member.apiRequest(t, http.MethodGet, "/api/v1/reports/summary?scope=all&from=2026-10-01&to=2026-10-31", nil, "")
	assert.Equal(t, http.StatusForbidden, response.StatusCode, "member all summary")
	response.Body.Close()

	createEntryAPI(t, admin, adminCSRF, "/api/v1/spendings", map[string]any{
		"amount": "5", "currency_code": "GBP", "entry_date": "2026-10-15",
	})
	response = admin.apiRequest(t, http.MethodGet, "/api/v1/reports/summary?scope=all&from=2026-10-01&to=2026-10-31&currency=RSD", nil, "")
	var incomplete summaryEnvelope
	decodeBody(t, response, &incomplete)
	assert.False(t, incomplete.Summary.Complete, "incomplete summary = %+v", incomplete.Summary)
	assert.Equal(t, 1, incomplete.Summary.MissingEntryCount, "incomplete summary = %+v", incomplete.Summary)
	require.Len(t, incomplete.Summary.MissingRates, 1, "incomplete summary = %+v", incomplete.Summary)
	assert.Equal(t, "GBP", incomplete.Summary.MissingRates[0].FromCurrencyCode, "missing rate")
	assert.Equal(t, "2026-10-15", incomplete.Summary.MissingRates[0].EntryDate, "missing rate")

	response = admin.apiRequest(t, http.MethodGet, "/api/v1/reports/summary?household_id="+homeID.String()+"&scope=all&from=2026-10-01&to=2026-10-31", nil, "")
	assert.Equal(t, http.StatusOK, response.StatusCode, "explicit household summary")
	response.Body.Close()

	response = admin.apiRequest(t, http.MethodGet, "/api/v1/reports/summary?scope=all", nil, "")
	assert.Equal(t, http.StatusUnprocessableEntity, response.StatusCode, "missing summary dates")
	response.Body.Close()

	response = admin.apiRequest(t, http.MethodGet, "/api/v1/reports/summary?scope=team&from=2026-10-01&to=2026-10-31", nil, "")
	assert.Equal(t, http.StatusBadRequest, response.StatusCode, "invalid summary scope")
	response.Body.Close()

	noHousehold := newTestClientWithPool(t, pool)
	noHousehold.registerHTML(t, "no-household@example.com")
	response = noHousehold.apiRequest(t, http.MethodGet, "/api/v1/reports/summary?from=2026-10-01&to=2026-10-31", nil, "")
	assert.Equal(t, http.StatusUnprocessableEntity, response.StatusCode, "summary without household")
	response.Body.Close()
}
