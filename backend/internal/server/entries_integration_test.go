//go:build integration

package server

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/google/uuid"

	"budgettracker/internal/testdb"
)

type entryEnvelope struct {
	Entry struct {
		ID           string  `json:"id"`
		Kind         string  `json:"kind"`
		HouseholdID  string  `json:"household_id"`
		UserID       *string `json:"user_id"`
		AmountMinor  int64   `json:"amount_minor"`
		Amount       string  `json:"amount"`
		CurrencyCode string  `json:"currency_code"`
		EntryDate    string  `json:"entry_date"`
		Description  *string `json:"description"`
		CreatedAt    string  `json:"created_at"`
		UpdatedAt    string  `json:"updated_at"`
	} `json:"entry"`
}

type entriesEnvelope struct {
	Entries []struct {
		ID           string  `json:"id"`
		Kind         string  `json:"kind"`
		UserID       *string `json:"user_id"`
		AmountMinor  int64   `json:"amount_minor"`
		Amount       string  `json:"amount"`
		CurrencyCode string  `json:"currency_code"`
		EntryDate    string  `json:"entry_date"`
		Description  *string `json:"description"`
	} `json:"entries"`
	Pagination struct {
		Page       int `json:"page"`
		PageSize   int `json:"page_size"`
		Total      int `json:"total"`
		TotalPages int `json:"total_pages"`
	} `json:"pagination"`
}

type currenciesEnvelope struct {
	Currencies []struct {
		Code       string `json:"code"`
		Name       string `json:"name"`
		MinorUnits int32  `json:"minor_units"`
	} `json:"currencies"`
}

func createAPIHousehold(t *testing.T, client *testClient, csrf, name string) uuid.UUID {
	t.Helper()
	response := client.apiRequest(t, http.MethodPost, "/api/v1/households", map[string]string{"name": name}, csrf)
	if !assert.Equal(t, http.StatusCreated, response.StatusCode, "create household") {
		t.Fatalf("body:\n%s", readBody(t, response))
	}
	var created householdEnvelope
	decodeBody(t, response, &created)
	id, err := uuid.Parse(created.Household.ID)
	require.NoError(t, err, "parse household ID")
	return id
}

func createEntryAPI(
	t *testing.T,
	client *testClient,
	csrf, path string,
	body map[string]any,
) entryEnvelope {
	t.Helper()
	response := client.apiRequest(t, http.MethodPost, path, body, csrf)
	if !assert.Equal(t, http.StatusCreated, response.StatusCode, "POST %s", path) {
		t.Fatalf("body:\n%s", readBody(t, response))
	}
	var created entryEnvelope
	decodeBody(t, response, &created)
	return created
}

func TestEntryAPIContractsAndAuthorization(t *testing.T) {
	pool := testdb.New(t)
	admin := newTestClientWithPool(t, pool)
	member := newTestClientWithPool(t, pool)
	outsider := newTestClientWithPool(t, pool)

	adminCSRF := admin.registerHTML(t, "admin@example.com")
	memberCSRF := member.registerHTML(t, "member@example.com")
	outsider.registerHTML(t, "outsider@example.com")

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

	response = admin.apiRequest(t, http.MethodGet, "/api/v1/currencies", nil, "")
	require.Equal(t, http.StatusOK, response.StatusCode, "currencies")
	var currencies currenciesEnvelope
	decodeBody(t, response, &currencies)
	assert.GreaterOrEqual(t, len(currencies.Currencies), 3, "currency count")

	adminIncome := createEntryAPI(t, admin, adminCSRF, "/api/v1/incomes", map[string]any{
		"amount": "10.50", "entry_date": "2026-10-01", "description": "Admin salary",
	})
	assert.Equal(t, "RSD", adminIncome.Entry.CurrencyCode, "default income = %+v", adminIncome.Entry)
	assert.Equal(t, "10.50", adminIncome.Entry.Amount, "default income = %+v", adminIncome.Entry)
	assert.Equal(t, "income", adminIncome.Entry.Kind, "income metadata = %+v", adminIncome.Entry)
	assert.NotNil(t, adminIncome.Entry.UserID, "income metadata = %+v", adminIncome.Entry)

	memberIncome := createEntryAPI(t, member, memberCSRF, "/api/v1/incomes", map[string]any{
		"amount": "20", "currency_code": "EUR", "entry_date": "2026-10-02",
	})
	assert.Equal(t, int64(2000), memberIncome.Entry.AmountMinor, "member income = %+v", memberIncome.Entry)
	assert.Equal(t, "20.00", memberIncome.Entry.Amount, "member income = %+v", memberIncome.Entry)

	sharedIncome := createEntryAPI(t, admin, adminCSRF, "/api/v1/incomes", map[string]any{
		"amount": "30", "entry_date": "2026-10-03", "household_wide": true,
	})
	assert.Nil(t, sharedIncome.Entry.UserID, "shared income user_id")

	response = admin.apiRequest(t, http.MethodGet, "/api/v1/incomes?scope=all&page_size=2", nil, "")
	require.Equal(t, http.StatusOK, response.StatusCode, "admin all list")
	var adminAll entriesEnvelope
	decodeBody(t, response, &adminAll)
	assert.Equal(t, 3, adminAll.Pagination.Total, "admin all pagination = %+v", adminAll.Pagination)
	assert.Equal(t, 2, adminAll.Pagination.TotalPages, "admin all pagination = %+v", adminAll.Pagination)
	require.Len(t, adminAll.Entries, 2, "admin all entries")
	assert.Equal(t, int64(3000), adminAll.Entries[0].AmountMinor, "newest amount")

	response = admin.apiRequest(t, http.MethodGet, "/api/v1/incomes?scope=mine", nil, "")
	var adminMine entriesEnvelope
	decodeBody(t, response, &adminMine)
	require.Equal(t, 1, adminMine.Pagination.Total, "admin mine = %+v", adminMine)
	require.Len(t, adminMine.Entries, 1)
	assert.Equal(t, int64(1050), adminMine.Entries[0].AmountMinor, "admin mine = %+v", adminMine)

	response = member.apiRequest(t, http.MethodGet, "/api/v1/incomes?scope=all", nil, "")
	assert.Equal(t, http.StatusForbidden, response.StatusCode, "member all list")
	assert.NoError(t, response.Body.Close(), "close response body")

	response = member.apiRequest(t, http.MethodGet, "/api/v1/incomes?scope=mine", nil, "")
	var memberMine entriesEnvelope
	decodeBody(t, response, &memberMine)
	require.Equal(t, 1, memberMine.Pagination.Total, "member mine = %+v", memberMine)
	require.Len(t, memberMine.Entries, 1)
	assert.Equal(t, int64(2000), memberMine.Entries[0].AmountMinor, "member mine = %+v", memberMine)

	response = admin.apiRequest(t, http.MethodGet, "/api/v1/incomes?scope=all&from=2026-10-02&to=2026-10-02", nil, "")
	var filtered entriesEnvelope
	decodeBody(t, response, &filtered)
	require.Equal(t, 1, filtered.Pagination.Total, "date filter = %+v", filtered)
	require.Len(t, filtered.Entries, 1)
	assert.Equal(t, int64(2000), filtered.Entries[0].AmountMinor, "date filter = %+v", filtered)

	adminEntryID := adminIncome.Entry.ID
	memberEntryID := memberIncome.Entry.ID
	sharedEntryID := sharedIncome.Entry.ID

	response = admin.apiRequest(t, http.MethodGet, "/api/v1/incomes/"+memberEntryID, nil, "")
	assert.Equal(t, http.StatusOK, response.StatusCode, "admin get member entry")
	assert.NoError(t, response.Body.Close(), "close response body")

	response = outsider.apiRequest(t, http.MethodGet, "/api/v1/incomes/"+adminEntryID, nil, "")
	assert.Equal(t, http.StatusNotFound, response.StatusCode, "outsider get entry")
	assert.NoError(t, response.Body.Close(), "close response body")

	response = member.apiRequest(t, http.MethodGet, "/api/v1/incomes/"+adminEntryID, nil, "")
	assert.Equal(t, http.StatusNotFound, response.StatusCode, "member get other entry")
	assert.NoError(t, response.Body.Close(), "close response body")

	response = admin.apiRequest(t, http.MethodPatch, "/api/v1/incomes/"+memberEntryID, map[string]any{"amount": "99"}, adminCSRF)
	assert.Equal(t, http.StatusForbidden, response.StatusCode, "admin patch member entry")
	assert.NoError(t, response.Body.Close(), "close response body")

	response = member.apiRequest(t, http.MethodPatch, "/api/v1/incomes/"+adminEntryID, map[string]any{"amount": "99"}, memberCSRF)
	assert.Equal(t, http.StatusNotFound, response.StatusCode, "member patch other entry")
	assert.NoError(t, response.Body.Close(), "close response body")

	response = member.apiRequest(t, http.MethodPatch, "/api/v1/incomes/"+memberEntryID, map[string]any{"amount": "25.50"}, memberCSRF)
	if !assert.Equal(t, http.StatusOK, response.StatusCode, "member patch own entry") {
		t.Fatalf("body = %s", readBody(t, response))
	}
	var updated entryEnvelope
	decodeBody(t, response, &updated)
	assert.Equal(t, int64(2550), updated.Entry.AmountMinor, "member patch own entry = %+v", updated.Entry)

	response = admin.apiRequest(t, http.MethodPatch, "/api/v1/incomes/"+sharedEntryID, map[string]any{"quantity": 1}, adminCSRF)
	assert.Equal(t, http.StatusBadRequest, response.StatusCode, "unknown patch field")
	assert.NoError(t, response.Body.Close(), "close response body")

	response = admin.apiRequest(t, http.MethodDelete, "/api/v1/incomes/"+memberEntryID, nil, adminCSRF)
	assert.Equal(t, http.StatusForbidden, response.StatusCode, "admin delete member entry")
	assert.NoError(t, response.Body.Close(), "close response body")

	response = member.apiRequest(t, http.MethodDelete, "/api/v1/incomes/"+memberEntryID, nil, memberCSRF)
	assert.Equal(t, http.StatusNoContent, response.StatusCode, "member delete own entry")
	assert.NoError(t, response.Body.Close(), "close response body")

	response = admin.apiRequest(t, http.MethodDelete, "/api/v1/incomes/"+sharedEntryID, nil, adminCSRF)
	assert.Equal(t, http.StatusNoContent, response.StatusCode, "admin delete shared entry")
	assert.NoError(t, response.Body.Close(), "close response body")
}

func TestEntryAPIValidationAndCSRF(t *testing.T) {
	pool := testdb.New(t)
	admin := newTestClientWithPool(t, pool)
	csrf := admin.registerHTML(t, "admin@example.com")
	createAPIHousehold(t, admin, csrf, "Home")

	response := admin.apiRequest(t, http.MethodPost, "/api/v1/incomes", map[string]any{"amount": "1.234"}, csrf)
	assert.Equal(t, http.StatusUnprocessableEntity, response.StatusCode, "invalid amount")
	var validation errorEnvelope
	decodeBody(t, response, &validation)
	assert.NotEmpty(t, validation.Error.Fields["amount"], "amount validation = %+v", validation.Error)

	response = admin.apiRequest(t, http.MethodPost, "/api/v1/incomes", map[string]any{
		"amount": "10", "entry_date": "not-a-date",
	}, csrf)
	assert.Equal(t, http.StatusUnprocessableEntity, response.StatusCode, "invalid date")
	assert.NoError(t, response.Body.Close(), "close response body")

	response = admin.apiRequest(t, http.MethodPost, "/api/v1/incomes", map[string]any{
		"amount": "10", "currency_code": "XXX",
	}, csrf)
	assert.Equal(t, http.StatusUnprocessableEntity, response.StatusCode, "invalid currency")
	var currencyValidation errorEnvelope
	decodeBody(t, response, &currencyValidation)
	assert.NotEmpty(t, currencyValidation.Error.Fields["currency_code"], "currency validation = %+v", currencyValidation.Error)

	response = admin.apiRequest(t, http.MethodPost, "/api/v1/incomes", map[string]any{"amount": "10"}, "")
	assert.Equal(t, http.StatusForbidden, response.StatusCode, "missing CSRF")
	assert.NoError(t, response.Body.Close(), "close response body")

	response = admin.apiRequest(t, http.MethodGet, "/api/v1/incomes?scope=team", nil, "")
	assert.Equal(t, http.StatusBadRequest, response.StatusCode, "invalid scope")
	assert.NoError(t, response.Body.Close(), "close response body")

	response = admin.apiRequest(t, http.MethodGet, "/api/v1/incomes?page=abc", nil, "")
	assert.Equal(t, http.StatusBadRequest, response.StatusCode, "invalid page")
	assert.NoError(t, response.Body.Close(), "close response body")

}

func TestEntryAPIRequiresActiveHousehold(t *testing.T) {
	pool := testdb.New(t)
	client := newTestClientWithPool(t, pool)
	csrf := client.registerHTML(t, "no-household@example.com")

	response := client.apiRequest(t, http.MethodGet, "/api/v1/incomes", nil, "")
	assert.Equal(t, http.StatusUnprocessableEntity, response.StatusCode, "list without household")
	var validation errorEnvelope
	decodeBody(t, response, &validation)
	assert.NotEmpty(t, validation.Error.Fields["household_id"], "household validation = %+v", validation.Error)

	response = client.apiRequest(t, http.MethodPost, "/api/v1/spendings", map[string]any{"amount": "1"}, csrf)
	assert.Equal(t, http.StatusUnprocessableEntity, response.StatusCode, "create without household")
	assert.NoError(t, response.Body.Close(), "close response body")
}
