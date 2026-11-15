//go:build integration

package server

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/google/uuid"

	"budgettracker/internal/testdb"
)

func rateIDByPair(t *testing.T, client *testClient, from, to string, monthly bool) int64 {
	t.Helper()
	var id int64
	if err := client.pool.QueryRow(context.Background(), `
		SELECT id FROM exchange_rates
		WHERE from_currency_code = $1 AND to_currency_code = $2 AND is_monthly = $3
		ORDER BY created_at DESC LIMIT 1`, from, to, monthly,
	).Scan(&id); err != nil {
		require.NoError(t, err, "look up rate %s/%s", from, to)
	}
	return id
}

func TestExchangeRateHTMLFlow(t *testing.T) {
	client := newTestClient(t)
	csrf := client.registerHTML(t, "admin@example.com")
	createAPIHousehold(t, client, csrf, "Home")

	response, err := client.follow.Get(client.baseURL + "/app/exchange-rates")
	require.NoError(t, err, "GET exchange rates")
	body := readBody(t, response)
	require.Equal(t, http.StatusOK, response.StatusCode, "empty rates; body:\n%s", body)
	assert.Contains(t, body, "No exchange rates yet")

	response = client.postForm(t, "/app/exchange-rates", url.Values{
		"csrf_token":         {csrf},
		"from_currency_code": {"EUR"},
		"to_currency_code":   {"RSD"},
		"rate":               {"117.25"},
		"rate_date":          {"2026-10-15"},
	})
	body = readBody(t, response)
	require.Equal(t, http.StatusOK, response.StatusCode, "create rate; body:\n%s", body)
	assert.Contains(t, body, "Exchange rate added.")
	assert.Contains(t, body, "1 EUR = 117.25 RSD")

	response = client.postForm(t, "/app/exchange-rates", url.Values{
		"csrf_token":         {csrf},
		"from_currency_code": {"EUR"},
		"to_currency_code":   {"RSD"},
		"rate":               {"118"},
		"rate_date":          {"2026-10-15"},
	})
	body = readBody(t, response)
	require.Equal(t, http.StatusUnprocessableEntity, response.StatusCode, "duplicate rate; body:\n%s", body)
	assert.Contains(t, body, "already exists")

	rateID := rateIDByPair(t, client, "EUR", "RSD", false)
	response, err = client.follow.Get(client.baseURL + "/app/exchange-rates/" + strconv.FormatInt(rateID, 10) + "/edit")
	require.NoError(t, err, "GET edit rate")
	body = readBody(t, response)
	require.Equal(t, http.StatusOK, response.StatusCode, "edit rate; body:\n%s", body)
	assert.Contains(t, body, "117.25")

	response = client.postForm(t, "/app/exchange-rates/"+strconv.FormatInt(rateID, 10), url.Values{
		"csrf_token":         {csrf},
		"from_currency_code": {"EUR"},
		"to_currency_code":   {"RSD"},
		"rate":               {"120.50"},
		"rate_date":          {"2026-10-16"},
	})
	body = readBody(t, response)
	require.Equal(t, http.StatusOK, response.StatusCode, "update rate; body:\n%s", body)
	assert.Contains(t, body, "Exchange rate updated.")
	assert.Contains(t, body, "120.5 RSD")

	response = client.postForm(t, "/app/exchange-rates/"+strconv.FormatInt(rateID, 10)+"/delete", url.Values{"csrf_token": {csrf}})
	body = readBody(t, response)
	require.Equal(t, http.StatusOK, response.StatusCode, "delete rate; body:\n%s", body)
	assert.Contains(t, body, "Exchange rate deleted.")
	assert.Contains(t, body, "No exchange rates yet")
}

func TestReportsHTMLFlow(t *testing.T) {
	pool := testdb.New(t)
	admin := newTestClientWithPool(t, pool)
	adminCSRF := admin.registerHTML(t, "admin@example.com")
	createAPIHousehold(t, admin, adminCSRF, "Home")
	createRateAPI(t, admin, adminCSRF, map[string]any{
		"from_currency_code": "EUR", "to_currency_code": "RSD",
		"rate": "117.5", "rate_date": "2026-10-15",
	})
	createEntryAPI(t, admin, adminCSRF, "/api/v1/incomes", map[string]any{
		"amount": "100", "entry_date": "2026-10-15",
	})
	createEntryAPI(t, admin, adminCSRF, "/api/v1/spendings", map[string]any{
		"amount": "10", "currency_code": "EUR", "entry_date": "2026-10-15",
	})

	response, err := admin.follow.Get(admin.baseURL + "/app/reports?from=2026-10-01&to=2026-10-31&currency=RSD&scope=all")
	require.NoError(t, err, "GET report")
	body := readBody(t, response)
	require.Equal(t, http.StatusOK, response.StatusCode, "report; body:\n%s", body)
	for _, want := range []string{"Reports", "100.00 RSD", "1,175.00 RSD", "-1,075.00 RSD", "All entries were converted."} {
		assert.Contains(t, body, want, "report")
	}

	createEntryAPI(t, admin, adminCSRF, "/api/v1/spendings", map[string]any{
		"amount": "5", "currency_code": "GBP", "entry_date": "2026-10-15",
	})
	response, err = admin.follow.Get(admin.baseURL + "/app/reports?from=2026-10-01&to=2026-10-31&currency=RSD")
	require.NoError(t, err, "GET incomplete report")
	body = readBody(t, response)
	assert.Contains(t, body, "Totals are incomplete", "incomplete report")
	assert.Contains(t, body, "GBP", "incomplete report")
	assert.Contains(t, body, "2026-10-15", "incomplete report")

	response, err = admin.follow.Get(admin.baseURL + "/app/reports?from=bad&to=2026-10-31")
	require.NoError(t, err, "GET invalid report")
	response.Body.Close()
	assert.Equal(t, http.StatusBadRequest, response.StatusCode, "invalid report date")

	// Members cannot widen scope through the query string.
	member := newTestClientWithPool(t, pool)
	memberCSRF := member.registerHTML(t, "member@example.com")
	homeID := reportActiveHouseholdID(t, admin)
	memberID := userIDByEmail(t, pool, "member@example.com")
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO household_memberships (household_id, user_id, role)
		VALUES ($1, $2, 'member')`, homeID, memberID,
	); err != nil {
		require.NoError(t, err, "add member")
	}
	response = member.postForm(t, "/app/active-household", url.Values{
		"csrf_token":   {memberCSRF},
		"household_id": {homeID.String()},
	})
	readBody(t, response)
	createEntryAPI(t, member, memberCSRF, "/api/v1/incomes", map[string]any{
		"amount": "20", "currency_code": "RSD", "entry_date": "2026-10-15",
	})

	response, err = member.follow.Get(member.baseURL + "/app/reports?from=2026-10-01&to=2026-10-31&currency=RSD&scope=all")
	require.NoError(t, err, "member GET report")
	body = readBody(t, response)
	require.Equal(t, http.StatusOK, response.StatusCode, "member report; body:\n%s", body)
	assert.Contains(t, body, "20.00 RSD", "member report missing own entry")
	assert.NotContains(t, body, "100.00 RSD", "member report leaked admin entry")
}

func reportActiveHouseholdID(t *testing.T, client *testClient) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := client.pool.QueryRow(context.Background(), `
		SELECT household_id FROM household_memberships
		WHERE user_id = (SELECT user_id FROM auth_credentials WHERE email_normalized = 'admin@example.com')
		ORDER BY created_at LIMIT 1`,
	).Scan(&id); err != nil {
		require.NoError(t, err, "look up household")
	}
	return id
}
