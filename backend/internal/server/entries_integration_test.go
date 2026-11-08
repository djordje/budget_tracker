//go:build integration

package server

import (
	"context"
	"net/http"
	"net/url"
	"testing"

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
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create household = %d, body:\n%s", response.StatusCode, readBody(t, response))
	}
	var created householdEnvelope
	decodeBody(t, response, &created)
	id, err := uuid.Parse(created.Household.ID)
	if err != nil {
		t.Fatalf("parse household ID: %v", err)
	}
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
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("POST %s = %d, body:\n%s", path, response.StatusCode, readBody(t, response))
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
		t.Fatalf("add member: %v", err)
	}

	response := member.postForm(t, "/app/active-household", url.Values{
		"csrf_token":   {memberCSRF},
		"household_id": {homeID.String()},
	})
	readBody(t, response)

	response = admin.apiRequest(t, http.MethodGet, "/api/v1/currencies", nil, "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("currencies = %d", response.StatusCode)
	}
	var currencies currenciesEnvelope
	decodeBody(t, response, &currencies)
	if len(currencies.Currencies) < 3 {
		t.Errorf("currency count = %d, want at least 3", len(currencies.Currencies))
	}

	adminIncome := createEntryAPI(t, admin, adminCSRF, "/api/v1/incomes", map[string]any{
		"amount": "10.50", "entry_date": "2026-10-01", "description": "Admin salary",
	})
	if adminIncome.Entry.CurrencyCode != "RSD" || adminIncome.Entry.Amount != "10.50" {
		t.Errorf("default income = %+v", adminIncome.Entry)
	}
	if adminIncome.Entry.Kind != "income" || adminIncome.Entry.UserID == nil {
		t.Errorf("income metadata = %+v", adminIncome.Entry)
	}

	memberIncome := createEntryAPI(t, member, memberCSRF, "/api/v1/incomes", map[string]any{
		"amount": "20", "currency_code": "EUR", "entry_date": "2026-10-02",
	})
	if memberIncome.Entry.AmountMinor != 2000 || memberIncome.Entry.Amount != "20.00" {
		t.Errorf("member income amount = %+v", memberIncome.Entry)
	}

	sharedIncome := createEntryAPI(t, admin, adminCSRF, "/api/v1/incomes", map[string]any{
		"amount": "30", "entry_date": "2026-10-03", "household_wide": true,
	})
	if sharedIncome.Entry.UserID != nil {
		t.Errorf("shared income user_id = %v, want null", sharedIncome.Entry.UserID)
	}

	response = admin.apiRequest(t, http.MethodGet, "/api/v1/incomes?scope=all&page_size=2", nil, "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("admin all list = %d", response.StatusCode)
	}
	var adminAll entriesEnvelope
	decodeBody(t, response, &adminAll)
	if adminAll.Pagination.Total != 3 || adminAll.Pagination.TotalPages != 2 || len(adminAll.Entries) != 2 {
		t.Errorf("admin all pagination = %+v, entries = %d", adminAll.Pagination, len(adminAll.Entries))
	}
	if adminAll.Entries[0].AmountMinor != 3000 {
		t.Errorf("newest amount = %d, want 3000", adminAll.Entries[0].AmountMinor)
	}

	response = admin.apiRequest(t, http.MethodGet, "/api/v1/incomes?scope=mine", nil, "")
	var adminMine entriesEnvelope
	decodeBody(t, response, &adminMine)
	if adminMine.Pagination.Total != 1 || adminMine.Entries[0].AmountMinor != 1050 {
		t.Errorf("admin mine = %+v", adminMine)
	}

	response = member.apiRequest(t, http.MethodGet, "/api/v1/incomes?scope=all", nil, "")
	if response.StatusCode != http.StatusForbidden {
		t.Errorf("member all list = %d, want 403", response.StatusCode)
	}
	response.Body.Close()

	response = member.apiRequest(t, http.MethodGet, "/api/v1/incomes?scope=mine", nil, "")
	var memberMine entriesEnvelope
	decodeBody(t, response, &memberMine)
	if memberMine.Pagination.Total != 1 || memberMine.Entries[0].AmountMinor != 2000 {
		t.Errorf("member mine = %+v", memberMine)
	}

	response = admin.apiRequest(t, http.MethodGet, "/api/v1/incomes?scope=all&from=2026-10-02&to=2026-10-02", nil, "")
	var filtered entriesEnvelope
	decodeBody(t, response, &filtered)
	if filtered.Pagination.Total != 1 || filtered.Entries[0].AmountMinor != 2000 {
		t.Errorf("date filter = %+v", filtered)
	}

	adminEntryID := adminIncome.Entry.ID
	memberEntryID := memberIncome.Entry.ID
	sharedEntryID := sharedIncome.Entry.ID

	response = admin.apiRequest(t, http.MethodGet, "/api/v1/incomes/"+memberEntryID, nil, "")
	if response.StatusCode != http.StatusOK {
		t.Errorf("admin get member entry = %d, want 200", response.StatusCode)
	}
	response.Body.Close()

	response = outsider.apiRequest(t, http.MethodGet, "/api/v1/incomes/"+adminEntryID, nil, "")
	if response.StatusCode != http.StatusNotFound {
		t.Errorf("outsider get entry = %d, want 404", response.StatusCode)
	}
	response.Body.Close()

	response = member.apiRequest(t, http.MethodGet, "/api/v1/incomes/"+adminEntryID, nil, "")
	if response.StatusCode != http.StatusNotFound {
		t.Errorf("member get other entry = %d, want 404", response.StatusCode)
	}
	response.Body.Close()

	response = admin.apiRequest(t, http.MethodPatch, "/api/v1/incomes/"+memberEntryID, map[string]any{"amount": "99"}, adminCSRF)
	if response.StatusCode != http.StatusForbidden {
		t.Errorf("admin patch member entry = %d, want 403", response.StatusCode)
	}
	response.Body.Close()

	response = member.apiRequest(t, http.MethodPatch, "/api/v1/incomes/"+adminEntryID, map[string]any{"amount": "99"}, memberCSRF)
	if response.StatusCode != http.StatusNotFound {
		t.Errorf("member patch other entry = %d, want 404", response.StatusCode)
	}
	response.Body.Close()

	response = member.apiRequest(t, http.MethodPatch, "/api/v1/incomes/"+memberEntryID, map[string]any{"amount": "25.50"}, memberCSRF)
	var updated entryEnvelope
	decodeBody(t, response, &updated)
	if response.StatusCode != http.StatusOK || updated.Entry.AmountMinor != 2550 {
		t.Errorf("member patch own entry = %d, %+v", response.StatusCode, updated.Entry)
	}

	response = admin.apiRequest(t, http.MethodPatch, "/api/v1/incomes/"+sharedEntryID, map[string]any{"quantity": 1}, adminCSRF)
	if response.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown patch field = %d, want 400", response.StatusCode)
	}
	response.Body.Close()

	response = admin.apiRequest(t, http.MethodDelete, "/api/v1/incomes/"+memberEntryID, nil, adminCSRF)
	if response.StatusCode != http.StatusForbidden {
		t.Errorf("admin delete member entry = %d, want 403", response.StatusCode)
	}
	response.Body.Close()

	response = member.apiRequest(t, http.MethodDelete, "/api/v1/incomes/"+memberEntryID, nil, memberCSRF)
	if response.StatusCode != http.StatusNoContent {
		t.Errorf("member delete own entry = %d, want 204", response.StatusCode)
	}
	response.Body.Close()

	response = admin.apiRequest(t, http.MethodDelete, "/api/v1/incomes/"+sharedEntryID, nil, adminCSRF)
	if response.StatusCode != http.StatusNoContent {
		t.Errorf("admin delete shared entry = %d, want 204", response.StatusCode)
	}
	response.Body.Close()
}

func TestEntryAPIValidationAndCSRF(t *testing.T) {
	pool := testdb.New(t)
	admin := newTestClientWithPool(t, pool)
	csrf := admin.registerHTML(t, "admin@example.com")
	createAPIHousehold(t, admin, csrf, "Home")

	response := admin.apiRequest(t, http.MethodPost, "/api/v1/incomes", map[string]any{"amount": "1.234"}, csrf)
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("invalid amount = %d, want 422", response.StatusCode)
	}
	var validation errorEnvelope
	decodeBody(t, response, &validation)
	if validation.Error.Fields["amount"] == "" {
		t.Errorf("amount validation = %+v", validation.Error)
	}

	response = admin.apiRequest(t, http.MethodPost, "/api/v1/incomes", map[string]any{
		"amount": "10", "entry_date": "not-a-date",
	}, csrf)
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("invalid date = %d, want 422", response.StatusCode)
	}
	response.Body.Close()

	response = admin.apiRequest(t, http.MethodPost, "/api/v1/incomes", map[string]any{
		"amount": "10", "currency_code": "XXX",
	}, csrf)
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("invalid currency = %d, want 422", response.StatusCode)
	}
	var currencyValidation errorEnvelope
	decodeBody(t, response, &currencyValidation)
	if currencyValidation.Error.Fields["currency_code"] == "" {
		t.Errorf("currency validation = %+v", currencyValidation.Error)
	}

	response = admin.apiRequest(t, http.MethodPost, "/api/v1/incomes", map[string]any{"amount": "10"}, "")
	if response.StatusCode != http.StatusForbidden {
		t.Errorf("missing CSRF = %d, want 403", response.StatusCode)
	}
	response.Body.Close()

	response = admin.apiRequest(t, http.MethodGet, "/api/v1/incomes?scope=team", nil, "")
	if response.StatusCode != http.StatusBadRequest {
		t.Errorf("invalid scope = %d, want 400", response.StatusCode)
	}
	response.Body.Close()

	response = admin.apiRequest(t, http.MethodGet, "/api/v1/incomes?page=abc", nil, "")
	if response.StatusCode != http.StatusBadRequest {
		t.Errorf("invalid page = %d, want 400", response.StatusCode)
	}
	response.Body.Close()

}

func TestEntryAPIRequiresActiveHousehold(t *testing.T) {
	pool := testdb.New(t)
	client := newTestClientWithPool(t, pool)
	csrf := client.registerHTML(t, "no-household@example.com")

	response := client.apiRequest(t, http.MethodGet, "/api/v1/incomes", nil, "")
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("list without household = %d, want 422", response.StatusCode)
	}
	var validation errorEnvelope
	decodeBody(t, response, &validation)
	if validation.Error.Fields["household_id"] == "" {
		t.Errorf("household validation = %+v", validation.Error)
	}

	response = client.apiRequest(t, http.MethodPost, "/api/v1/spendings", map[string]any{"amount": "1"}, csrf)
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("create without household = %d, want 422", response.StatusCode)
	}
	response.Body.Close()
}
