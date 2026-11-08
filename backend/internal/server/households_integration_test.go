//go:build integration

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"budgettracker/internal/testdb"
)

type householdEnvelope struct {
	Household struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Role      string `json:"role"`
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at"`
	} `json:"household"`
}

type householdsEnvelope struct {
	Households []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Role string `json:"role"`
	} `json:"households"`
}

type membersEnvelope struct {
	Members []struct {
		UserID    string `json:"user_id"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
		Email     string `json:"email"`
		Role      string `json:"role"`
	} `json:"members"`
}

type optionsEnvelope struct {
	Options struct {
		DefaultIncomeCurrencyCode   string `json:"default_income_currency_code"`
		DefaultSpendingCurrencyCode string `json:"default_spending_currency_code"`
		DefaultDisplayCurrencyCode  string `json:"default_display_currency_code"`
	} `json:"options"`
}

type errorEnvelope struct {
	Error struct {
		Code    string            `json:"code"`
		Message string            `json:"message"`
		Fields  map[string]string `json:"fields"`
	} `json:"error"`
}

func (c *testClient) apiRequest(t *testing.T, method, path string, body any, csrf string) *http.Response {
	t.Helper()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}

	request, err := http.NewRequest(method, c.baseURL+path, reader)
	if err != nil {
		t.Fatalf("create API request: %v", err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}

	response, err := c.follow.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return response
}

func (c *testClient) registerHTML(t *testing.T, email string) string {
	t.Helper()
	csrf := c.csrfToken(t, "/register")
	values := registerForm(csrf)
	values.Set("email", email)
	response := c.postForm(t, "/register", values)
	body := readBody(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("register %s = %d, body:\n%s", email, response.StatusCode, body)
	}
	return csrf
}

func decodeBody(t *testing.T, response *http.Response, destination any) {
	t.Helper()
	defer response.Body.Close()
	if err := json.NewDecoder(response.Body).Decode(destination); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
}

func userIDByEmail(t *testing.T, pool *pgxpool.Pool, email string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := pool.QueryRow(context.Background(), `
		SELECT user_id FROM auth_credentials WHERE email_normalized = $1`, email,
	).Scan(&id); err != nil {
		t.Fatalf("look up user %s: %v", email, err)
	}
	return id
}

func TestHouseholdAPIAuthorizationAndContracts(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	ada := newTestClientWithPool(t, pool)
	grace := newTestClientWithPool(t, pool)
	guest := newTestClientWithPool(t, pool)

	adaCSRF := ada.registerHTML(t, "ada@example.com")
	graceCSRF := grace.registerHTML(t, "grace@example.com")

	response := ada.apiRequest(t, http.MethodPost, "/api/v1/households", map[string]string{"name": "  Home  "}, adaCSRF)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create household status = %d, body:\n%s", response.StatusCode, readBody(t, response))
	}
	if location := response.Header.Get("Location"); location == "" {
		t.Error("create household response has no Location header")
	}
	var created householdEnvelope
	decodeBody(t, response, &created)
	if created.Household.Name != "Home" || created.Household.Role != "admin" {
		t.Errorf("created household = %+v", created.Household)
	}
	if created.Household.ID == "" || created.Household.CreatedAt == "" {
		t.Errorf("created household is missing fields: %+v", created.Household)
	}
	homeID, err := uuid.Parse(created.Household.ID)
	if err != nil {
		t.Fatalf("parse created household ID: %v", err)
	}

	response = ada.apiRequest(t, http.MethodGet, "/api/v1/households", nil, "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("list households status = %d", response.StatusCode)
	}
	var listed householdsEnvelope
	decodeBody(t, response, &listed)
	if len(listed.Households) != 1 || listed.Households[0].ID != homeID.String() {
		t.Errorf("listed households = %+v", listed.Households)
	}

	response = ada.apiRequest(t, http.MethodGet, "/api/v1/households/"+homeID.String(), nil, "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("get household status = %d", response.StatusCode)
	}
	var fetched householdEnvelope
	decodeBody(t, response, &fetched)
	if fetched.Household.ID != homeID.String() {
		t.Errorf("fetched household ID = %q", fetched.Household.ID)
	}

	response = grace.apiRequest(t, http.MethodPost, "/api/v1/households", map[string]string{"name": "Other home"}, graceCSRF)
	var graceCreated householdEnvelope
	decodeBody(t, response, &graceCreated)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create other household status = %d", response.StatusCode)
	}
	otherID, err := uuid.Parse(graceCreated.Household.ID)
	if err != nil {
		t.Fatalf("parse other household ID: %v", err)
	}

	response = ada.apiRequest(t, http.MethodGet, "/api/v1/households/"+otherID.String(), nil, "")
	if response.StatusCode != http.StatusNotFound {
		t.Errorf("cross-household get status = %d, want 404", response.StatusCode)
	}
	response.Body.Close()

	graceID := userIDByEmail(t, pool, "grace@example.com")
	if _, err := pool.Exec(ctx, `
		INSERT INTO household_memberships (household_id, user_id, role)
		VALUES ($1, $2, 'member')`, homeID, graceID,
	); err != nil {
		t.Fatalf("add member: %v", err)
	}

	response = grace.apiRequest(t, http.MethodGet, "/api/v1/households/"+homeID.String(), nil, "")
	if response.StatusCode != http.StatusOK {
		t.Errorf("member get household status = %d, want 200", response.StatusCode)
	}
	var memberView householdEnvelope
	decodeBody(t, response, &memberView)
	if memberView.Household.Role != "member" {
		t.Errorf("member role = %q, want member", memberView.Household.Role)
	}

	response = grace.apiRequest(t, http.MethodGet, "/api/v1/households/"+homeID.String()+"/members", nil, "")
	if response.StatusCode != http.StatusForbidden {
		t.Errorf("member list members status = %d, want 403", response.StatusCode)
	}
	response.Body.Close()

	response = ada.apiRequest(t, http.MethodGet, "/api/v1/households/"+homeID.String()+"/members", nil, "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("admin list members status = %d", response.StatusCode)
	}
	var members membersEnvelope
	decodeBody(t, response, &members)
	if len(members.Members) != 2 {
		t.Errorf("member count = %d, want 2", len(members.Members))
	}

	response = ada.apiRequest(t, http.MethodGet, "/api/v1/households/"+homeID.String()+"/options", nil, "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("get options status = %d", response.StatusCode)
	}
	var options optionsEnvelope
	decodeBody(t, response, &options)
	if options.Options.DefaultIncomeCurrencyCode != "RSD" {
		t.Errorf("default income currency = %q", options.Options.DefaultIncomeCurrencyCode)
	}

	response = ada.apiRequest(t, http.MethodPost, "/api/v1/households", map[string]string{"name": ""}, adaCSRF)
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("invalid household status = %d, want 422", response.StatusCode)
	}
	var validation errorEnvelope
	decodeBody(t, response, &validation)
	if validation.Error.Code != "validation_failed" || validation.Error.Fields["name"] == "" {
		t.Errorf("validation error = %+v", validation.Error)
	}

	response = ada.apiRequest(t, http.MethodPost, "/api/v1/households", map[string]string{"name": "No CSRF"}, "")
	if response.StatusCode != http.StatusForbidden {
		t.Errorf("missing CSRF status = %d, want 403", response.StatusCode)
	}
	var csrfError errorEnvelope
	decodeBody(t, response, &csrfError)
	if csrfError.Error.Code != "csrf_failed" {
		t.Errorf("CSRF error code = %q", csrfError.Error.Code)
	}

	response = guest.apiRequest(t, http.MethodGet, "/api/v1/households", nil, "")
	if response.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated list status = %d, want 401", response.StatusCode)
	}
	var unauthorized errorEnvelope
	decodeBody(t, response, &unauthorized)
	if unauthorized.Error.Code != "unauthenticated" {
		t.Errorf("unauthenticated error code = %q", unauthorized.Error.Code)
	}

	response = ada.apiRequest(t, http.MethodGet, "/api/v1/households/not-a-uuid", nil, "")
	if response.StatusCode != http.StatusNotFound {
		t.Errorf("invalid household ID status = %d, want 404", response.StatusCode)
	}
	response.Body.Close()
}
