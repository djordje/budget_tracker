//go:build integration

package server

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

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
		t.Fatalf("look up rate %s/%s: %v", from, to, err)
	}
	return id
}

func TestExchangeRateHTMLFlow(t *testing.T) {
	client := newTestClient(t)
	csrf := client.registerHTML(t, "admin@example.com")
	createAPIHousehold(t, client, csrf, "Home")

	response, err := client.follow.Get(client.baseURL + "/app/exchange-rates")
	if err != nil {
		t.Fatalf("GET exchange rates: %v", err)
	}
	body := readBody(t, response)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "No exchange rates yet") {
		t.Fatalf("empty rates = %d, body:\n%s", response.StatusCode, body)
	}

	response = client.postForm(t, "/app/exchange-rates", url.Values{
		"csrf_token":         {csrf},
		"from_currency_code": {"EUR"},
		"to_currency_code":   {"RSD"},
		"rate":               {"117.25"},
		"rate_date":          {"2026-10-15"},
	})
	body = readBody(t, response)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "Exchange rate added.") || !strings.Contains(body, "1 EUR = 117.25 RSD") {
		t.Fatalf("create rate = %d, body:\n%s", response.StatusCode, body)
	}

	response = client.postForm(t, "/app/exchange-rates", url.Values{
		"csrf_token":         {csrf},
		"from_currency_code": {"EUR"},
		"to_currency_code":   {"RSD"},
		"rate":               {"118"},
		"rate_date":          {"2026-10-15"},
	})
	body = readBody(t, response)
	if response.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "already exists") {
		t.Fatalf("duplicate rate = %d, body:\n%s", response.StatusCode, body)
	}

	rateID := rateIDByPair(t, client, "EUR", "RSD", false)
	response, err = client.follow.Get(client.baseURL + "/app/exchange-rates/" + strconv.FormatInt(rateID, 10) + "/edit")
	if err != nil {
		t.Fatalf("GET edit rate: %v", err)
	}
	body = readBody(t, response)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "117.25") {
		t.Fatalf("edit rate = %d, body:\n%s", response.StatusCode, body)
	}

	response = client.postForm(t, "/app/exchange-rates/"+strconv.FormatInt(rateID, 10), url.Values{
		"csrf_token":         {csrf},
		"from_currency_code": {"EUR"},
		"to_currency_code":   {"RSD"},
		"rate":               {"120.50"},
		"rate_date":          {"2026-10-16"},
	})
	body = readBody(t, response)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "Exchange rate updated.") || !strings.Contains(body, "120.5 RSD") {
		t.Fatalf("update rate = %d, body:\n%s", response.StatusCode, body)
	}

	response = client.postForm(t, "/app/exchange-rates/"+strconv.FormatInt(rateID, 10)+"/delete", url.Values{"csrf_token": {csrf}})
	body = readBody(t, response)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "Exchange rate deleted.") || !strings.Contains(body, "No exchange rates yet") {
		t.Fatalf("delete rate = %d, body:\n%s", response.StatusCode, body)
	}
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
	if err != nil {
		t.Fatalf("GET report: %v", err)
	}
	body := readBody(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("report = %d, body:\n%s", response.StatusCode, body)
	}
	for _, want := range []string{"Reports", "100.00 RSD", "1,175.00 RSD", "-1,075.00 RSD", "All entries were converted."} {
		if !strings.Contains(body, want) {
			t.Errorf("report does not contain %q", want)
		}
	}

	createEntryAPI(t, admin, adminCSRF, "/api/v1/spendings", map[string]any{
		"amount": "5", "currency_code": "GBP", "entry_date": "2026-10-15",
	})
	response, err = admin.follow.Get(admin.baseURL + "/app/reports?from=2026-10-01&to=2026-10-31&currency=RSD")
	if err != nil {
		t.Fatalf("GET incomplete report: %v", err)
	}
	body = readBody(t, response)
	if !strings.Contains(body, "Totals are incomplete") || !strings.Contains(body, "GBP") || !strings.Contains(body, "2026-10-15") {
		t.Errorf("incomplete report missing warning:\n%s", body)
	}

	response, err = admin.follow.Get(admin.baseURL + "/app/reports?from=bad&to=2026-10-31")
	if err != nil {
		t.Fatalf("GET invalid report: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Errorf("invalid report date = %d, want 400", response.StatusCode)
	}

	// Members cannot widen scope through the query string.
	member := newTestClientWithPool(t, pool)
	memberCSRF := member.registerHTML(t, "member@example.com")
	homeID := reportActiveHouseholdID(t, admin)
	memberID := userIDByEmail(t, pool, "member@example.com")
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO household_memberships (household_id, user_id, role)
		VALUES ($1, $2, 'member')`, homeID, memberID,
	); err != nil {
		t.Fatalf("add member: %v", err)
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
	if err != nil {
		t.Fatalf("member GET report: %v", err)
	}
	body = readBody(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("member report = %d, body:\n%s", response.StatusCode, body)
	}
	if !strings.Contains(body, "20.00 RSD") {
		t.Errorf("member report missing own entry:\n%s", body)
	}
	if strings.Contains(body, "100.00 RSD") {
		t.Errorf("member report leaked admin entry:\n%s", body)
	}
}

func reportActiveHouseholdID(t *testing.T, client *testClient) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := client.pool.QueryRow(context.Background(), `
		SELECT household_id FROM household_memberships
		WHERE user_id = (SELECT user_id FROM auth_credentials WHERE email_normalized = 'admin@example.com')
		ORDER BY created_at LIMIT 1`,
	).Scan(&id); err != nil {
		t.Fatalf("look up household: %v", err)
	}
	return id
}
