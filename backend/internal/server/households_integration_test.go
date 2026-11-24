//go:build integration

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
		require.NoError(t, err, "marshal request body")
		reader = bytes.NewReader(encoded)
	}

	request, err := http.NewRequest(method, c.baseURL+path, reader)
	require.NoError(t, err, "create API request")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}

	response, err := c.follow.Do(request)
	require.NoError(t, err, "%s %s", method, path)
	return response
}

func (c *testClient) registerHTML(t *testing.T, email string) string {
	t.Helper()
	csrf := c.csrfToken(t, "/register")
	values := registerForm(csrf)
	values.Set("email", email)
	response := c.postForm(t, "/register", values)
	if !assert.Equal(t, http.StatusOK, response.StatusCode, "register %s", email) {
		t.Fatalf("body:\n%s", readBody(t, response))
	}
	readBody(t, response)
	return csrf
}

func decodeBody(t *testing.T, response *http.Response, destination any) {
	t.Helper()
	defer func() {
		assert.NoError(t, response.Body.Close(), "close response body")
	}()
	require.NoError(t, json.NewDecoder(response.Body).Decode(destination), "decode response body")
}

func userIDByEmail(t *testing.T, pool *pgxpool.Pool, email string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := pool.QueryRow(context.Background(), `
		SELECT user_id FROM auth_credentials WHERE email_normalized = $1`, email,
	).Scan(&id); err != nil {
		require.NoError(t, err, "look up user %s", email)
	}
	return id
}

func TestHouseholdAPIRejectsExplicitlyInvalidCurrency(t *testing.T) {
	client := newTestClient(t)
	csrf := client.registerHTML(t, "ada@example.com")

	for _, tc := range []struct {
		name  string
		value any
	}{
		{name: "empty string", value: ""},
		{name: "whitespace", value: " \t "},
		{name: "null", value: nil},
		{name: "number", value: 42},
		{name: "unknown code", value: "XXX"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := client.apiRequest(t, http.MethodPost, "/api/v1/households", map[string]any{
				"name": "Home", "currency_code": tc.value,
			}, csrf)
			var validation errorEnvelope
			decodeBody(t, response, &validation)
			require.Equal(t, http.StatusUnprocessableEntity, response.StatusCode)
			assert.Equal(t, "validation_failed", validation.Error.Code)
			assert.NotEmpty(t, validation.Error.Fields["currency_code"])
		})
	}

	response := client.apiRequest(t, http.MethodGet, "/api/v1/households", nil, "")
	var listed householdsEnvelope
	decodeBody(t, response, &listed)
	require.Equal(t, http.StatusOK, response.StatusCode)
	assert.Empty(t, listed.Households, "invalid requests must not create households")
}

func TestHouseholdAPIAcceptsNormalizedCurrency(t *testing.T) {
	client := newTestClient(t)
	csrf := client.registerHTML(t, "ada@example.com")

	response := client.apiRequest(t, http.MethodPost, "/api/v1/households", map[string]string{
		"name": "Euro home", "currency_code": " eur ",
	}, csrf)
	var created householdEnvelope
	decodeBody(t, response, &created)
	require.Equal(t, http.StatusCreated, response.StatusCode, "create household status")
	require.NotEmpty(t, created.Household.ID, "created household ID")

	response = client.apiRequest(t, http.MethodGet, "/api/v1/households/"+created.Household.ID+"/options", nil, "")
	var options optionsEnvelope
	decodeBody(t, response, &options)
	require.Equal(t, http.StatusOK, response.StatusCode, "get options status")
	assert.Equal(t, "EUR", options.Options.DefaultIncomeCurrencyCode, "default income currency")
	assert.Equal(t, "EUR", options.Options.DefaultSpendingCurrencyCode, "default spending currency")
	assert.Equal(t, "EUR", options.Options.DefaultDisplayCurrencyCode, "default display currency")
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
	if !assert.Equal(t, http.StatusCreated, response.StatusCode, "create household status") {
		t.Fatalf("body:\n%s", readBody(t, response))
	}
	assert.NotEmpty(t, response.Header.Get("Location"), "create household response has no Location header")
	var created householdEnvelope
	decodeBody(t, response, &created)
	assert.Equal(t, "Home", created.Household.Name, "created household = %+v", created.Household)
	assert.Equal(t, "admin", created.Household.Role, "created household = %+v", created.Household)
	assert.NotEmpty(t, created.Household.ID, "created household = %+v", created.Household)
	assert.NotEmpty(t, created.Household.CreatedAt, "created household = %+v", created.Household)
	homeID, err := uuid.Parse(created.Household.ID)
	require.NoError(t, err, "parse created household ID")

	response = ada.apiRequest(t, http.MethodGet, "/api/v1/households", nil, "")
	require.Equal(t, http.StatusOK, response.StatusCode, "list households status")
	var listed householdsEnvelope
	decodeBody(t, response, &listed)
	require.Len(t, listed.Households, 1, "listed households")
	assert.Equal(t, homeID.String(), listed.Households[0].ID, "listed households = %+v", listed.Households)

	response = ada.apiRequest(t, http.MethodGet, "/api/v1/households/"+homeID.String(), nil, "")
	require.Equal(t, http.StatusOK, response.StatusCode, "get household status")
	var fetched householdEnvelope
	decodeBody(t, response, &fetched)
	assert.Equal(t, homeID.String(), fetched.Household.ID, "fetched household ID")

	response = grace.apiRequest(t, http.MethodPost, "/api/v1/households", map[string]string{"name": "Other home"}, graceCSRF)
	var graceCreated householdEnvelope
	decodeBody(t, response, &graceCreated)
	require.Equal(t, http.StatusCreated, response.StatusCode, "create other household status")
	otherID, err := uuid.Parse(graceCreated.Household.ID)
	require.NoError(t, err, "parse other household ID")

	response = ada.apiRequest(t, http.MethodGet, "/api/v1/households/"+otherID.String(), nil, "")
	assert.Equal(t, http.StatusNotFound, response.StatusCode, "cross-household get status")
	assert.NoError(t, response.Body.Close(), "close response body")

	graceID := userIDByEmail(t, pool, "grace@example.com")
	if _, err := pool.Exec(ctx, `
		INSERT INTO household_memberships (household_id, user_id, role)
		VALUES ($1, $2, 'member')`, homeID, graceID,
	); err != nil {
		require.NoError(t, err, "add member")
	}

	response = grace.apiRequest(t, http.MethodGet, "/api/v1/households/"+homeID.String(), nil, "")
	assert.Equal(t, http.StatusOK, response.StatusCode, "member get household status")
	var memberView householdEnvelope
	decodeBody(t, response, &memberView)
	assert.Equal(t, "member", memberView.Household.Role, "member role")

	response = grace.apiRequest(t, http.MethodGet, "/api/v1/households/"+homeID.String()+"/members", nil, "")
	assert.Equal(t, http.StatusForbidden, response.StatusCode, "member list members status")
	assert.NoError(t, response.Body.Close(), "close response body")

	response = ada.apiRequest(t, http.MethodGet, "/api/v1/households/"+homeID.String()+"/members", nil, "")
	require.Equal(t, http.StatusOK, response.StatusCode, "admin list members status")
	var members membersEnvelope
	decodeBody(t, response, &members)
	assert.Len(t, members.Members, 2, "member count")

	response = ada.apiRequest(t, http.MethodGet, "/api/v1/households/"+homeID.String()+"/options", nil, "")
	require.Equal(t, http.StatusOK, response.StatusCode, "get options status")
	var options optionsEnvelope
	decodeBody(t, response, &options)
	assert.Equal(t, "RSD", options.Options.DefaultIncomeCurrencyCode, "default income currency")

	response = ada.apiRequest(t, http.MethodPost, "/api/v1/households", map[string]string{"name": ""}, adaCSRF)
	assert.Equal(t, http.StatusUnprocessableEntity, response.StatusCode, "invalid household status")
	var validation errorEnvelope
	decodeBody(t, response, &validation)
	assert.Equal(t, "validation_failed", validation.Error.Code, "validation error = %+v", validation.Error)
	assert.NotEmpty(t, validation.Error.Fields["name"], "validation error = %+v", validation.Error)

	response = ada.apiRequest(t, http.MethodPost, "/api/v1/households", map[string]string{"name": "No CSRF"}, "")
	assert.Equal(t, http.StatusForbidden, response.StatusCode, "missing CSRF status")
	var csrfError errorEnvelope
	decodeBody(t, response, &csrfError)
	assert.Equal(t, "csrf_failed", csrfError.Error.Code, "CSRF error code")

	response = guest.apiRequest(t, http.MethodGet, "/api/v1/households", nil, "")
	assert.Equal(t, http.StatusUnauthorized, response.StatusCode, "unauthenticated list status")
	var unauthorized errorEnvelope
	decodeBody(t, response, &unauthorized)
	assert.Equal(t, "unauthenticated", unauthorized.Error.Code, "unauthenticated error code")

	response = ada.apiRequest(t, http.MethodGet, "/api/v1/households/not-a-uuid", nil, "")
	assert.Equal(t, http.StatusNotFound, response.StatusCode, "invalid household ID status")
	assert.NoError(t, response.Body.Close(), "close response body")
}
