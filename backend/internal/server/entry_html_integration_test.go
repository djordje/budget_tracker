//go:build integration

package server

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func entryIDByDescription(t *testing.T, client *testClient, table, description string) int64 {
	t.Helper()
	var id int64
	if err := client.pool.QueryRow(context.Background(), fmt.Sprintf(`
		SELECT id FROM %s WHERE description = $1 ORDER BY created_at DESC LIMIT 1`, table),
		description,
	).Scan(&id); err != nil {
		require.NoError(t, err, "look up %s entry %q", table, description)
	}
	return id
}

func TestEntryHTMLFlow(t *testing.T) {
	client := newTestClient(t)
	csrf := client.registerHTML(t, "ada@example.com")
	homeID := createHouseholdHTML(t, client, csrf, "Home")
	base := "/app/" + homeID.String()

	response, err := client.follow.Get(client.baseURL + base + "/entries")
	require.NoError(t, err, "GET entries")
	body := readBody(t, response)
	require.Equal(t, http.StatusOK, response.StatusCode, "empty entries; body:\n%s", body)
	assert.Contains(t, body, "No Spending entries yet")

	response, err = client.follow.Get(client.baseURL + base + "/spendings/new")
	require.NoError(t, err, "GET spending form")
	body = readBody(t, response)
	for _, want := range []string{"New spending", `name="amount"`, "RSD", "Whole household"} {
		assert.Contains(t, body, want, "spending form")
	}

	response = client.postForm(t, base+"/spendings", url.Values{
		"csrf_token":    {csrf},
		"amount":        {"12.34"},
		"currency_code": {"EUR"},
		"entry_date":    {"2026-10-05"},
		"description":   {"Coffee"},
		"attribution":   {"self"},
	})
	body = readBody(t, response)
	require.Equal(t, http.StatusOK, response.StatusCode, "create spending; body:\n%s", body)
	for _, want := range []string{"Entry added.", "Coffee", "-12.34 EUR"} {
		assert.Contains(t, body, want, "entries list")
	}

	spendingID := entryIDByDescription(t, client, "spendings", "Coffee")
	response, err = client.follow.Get(client.baseURL + base + "/spendings/" + strconv.FormatInt(spendingID, 10) + "/edit")
	require.NoError(t, err, "GET edit spending")
	body = readBody(t, response)
	require.Equal(t, http.StatusOK, response.StatusCode, "edit spending; body:\n%s", body)
	assert.Contains(t, body, "Coffee")
	assert.Contains(t, body, `value="12.34"`)

	response = client.postForm(t, base+"/spendings/"+strconv.FormatInt(spendingID, 10), url.Values{
		"csrf_token":    {csrf},
		"amount":        {"15.00"},
		"currency_code": {"EUR"},
		"entry_date":    {"2026-10-06"},
		"description":   {"Lunch"},
	})
	body = readBody(t, response)
	require.Equal(t, http.StatusOK, response.StatusCode, "update spending; body:\n%s", body)
	assert.Contains(t, body, "Entry updated.")
	assert.Contains(t, body, "Lunch")

	response = client.postForm(t, base+"/spendings/"+strconv.FormatInt(spendingID, 10)+"/delete", url.Values{"csrf_token": {csrf}})
	body = readBody(t, response)
	require.Equal(t, http.StatusOK, response.StatusCode, "delete spending; body:\n%s", body)
	assert.Contains(t, body, "Entry deleted.")
	assert.NotContains(t, body, "Lunch")

	response = client.postForm(t, base+"/incomes", url.Values{
		"csrf_token":    {csrf},
		"amount":        {"100"},
		"currency_code": {"RSD"},
		"entry_date":    {"2026-10-01"},
		"description":   {"Salary"},
	})
	readBody(t, response)

	response, err = client.follow.Get(client.baseURL + base + "/entries?type=income")
	require.NoError(t, err, "GET income entries")
	body = readBody(t, response)
	assert.Contains(t, body, "Salary", "income list")
	assert.Contains(t, body, "+100.00 RSD", "income list")

	// Admin-only household-wide entry.
	response = client.postForm(t, base+"/incomes", url.Values{
		"csrf_token":    {csrf},
		"amount":        {"500"},
		"currency_code": {"RSD"},
		"entry_date":    {"2026-10-02"},
		"description":   {"Rent"},
		"attribution":   {"household"},
	})
	readBody(t, response)
	response, err = client.follow.Get(client.baseURL + base + "/entries?type=income&scope=all")
	require.NoError(t, err, "GET all income entries")
	body = readBody(t, response)
	assert.Contains(t, body, "Household", "admin all list")
	assert.Contains(t, body, "Rent", "admin all list")

	member := newTestClientWithPool(t, client.pool)
	memberCSRF := member.registerHTML(t, "grace@example.com")
	graceID := userIDByEmail(t, client.pool, "grace@example.com")
	if _, err := client.pool.Exec(context.Background(), `
		INSERT INTO household_memberships (household_id, user_id, role)
		VALUES ($1, $2, 'member')`, homeID, graceID,
	); err != nil {
		require.NoError(t, err, "add member")
	}
	response, err = member.follow.Get(member.baseURL + base + "/spendings/new")
	require.NoError(t, err, "member GET spending form")
	body = readBody(t, response)
	assert.NotContains(t, body, "Whole household", "member spending form attribution")
	assert.Contains(t, body, "Your entry", "member spending form attribution")

	response = member.postForm(t, base+"/spendings", url.Values{
		"csrf_token":    {memberCSRF},
		"amount":        {"9.99"},
		"currency_code": {"EUR"},
		"entry_date":    {"2026-10-07"},
		"description":   {"Member coffee"},
		"attribution":   {"household"},
	})
	body = readBody(t, response)
	require.Equal(t, http.StatusForbidden, response.StatusCode, "member household-wide create; body:\n%s", body)
	assert.Contains(t, body, "do not have permission")

	response = member.postForm(t, base+"/spendings", url.Values{
		"csrf_token":    {memberCSRF},
		"amount":        {"9.99"},
		"currency_code": {"EUR"},
		"entry_date":    {"2026-10-07"},
		"description":   {"Member coffee"},
	})
	readBody(t, response)
	memberSpendingID := entryIDByDescription(t, member, "spendings", "Member coffee")

	response, err = client.follow.Get(client.baseURL + base + "/spendings/" + strconv.FormatInt(memberSpendingID, 10) + "/edit")
	require.NoError(t, err, "admin GET member entry edit")
	assert.NoError(t, response.Body.Close(), "close response body")
	assert.Equal(t, http.StatusForbidden, response.StatusCode, "admin edit member entry")

	response, err = member.follow.Get(member.baseURL + base + "/incomes/" + strconv.FormatInt(entryIDByDescription(t, client, "incomes", "Salary"), 10) + "/edit")
	require.NoError(t, err, "member GET admin entry edit")
	assert.NoError(t, response.Body.Close(), "close response body")
	assert.Equal(t, http.StatusNotFound, response.StatusCode, "member edit admin entry")

	// Validation preserves values and reports the amount error.
	response = member.postForm(t, base+"/spendings", url.Values{
		"csrf_token":    {memberCSRF},
		"amount":        {"1.234"},
		"currency_code": {"EUR"},
		"entry_date":    {"2026-10-07"},
		"description":   {"Too precise"},
	})
	body = readBody(t, response)
	require.Equal(t, http.StatusUnprocessableEntity, response.StatusCode, "invalid amount; body:\n%s", body)
	assert.Contains(t, body, "at most 2 decimal places")
	assert.Contains(t, body, "Too precise")
}

func TestEntryHTMLRequiresHouseholdAndCSRF(t *testing.T) {
	client := newTestClient(t)
	csrf := client.registerHTML(t, "ada@example.com")

	response, err := client.noRedirect.Get(client.baseURL + "/app/00000000-0000-0000-0000-000000000001/spendings/new")
	require.NoError(t, err, "GET spending form without household")
	assert.NoError(t, response.Body.Close(), "close response body")
	assert.Equal(t, http.StatusNotFound, response.StatusCode, "no-household form")
	assert.Empty(t, response.Header.Get("Location"))

	homeID := createHouseholdHTML(t, client, csrf, "Home")
	base := "/app/" + homeID.String()
	response = client.postForm(t, base+"/spendings", url.Values{
		"amount": {"1"}, "currency_code": {"RSD"}, "entry_date": {"2026-10-01"},
	})
	assert.NoError(t, response.Body.Close(), "close response body")
	assert.Equal(t, http.StatusForbidden, response.StatusCode, "missing CSRF")
}

func TestEntryHTMLPaginationAndFilters(t *testing.T) {
	client := newTestClient(t)
	csrf := client.registerHTML(t, "ada@example.com")
	homeID := createHouseholdHTML(t, client, csrf, "Home")
	base := "/app/" + homeID.String()
	userID := userIDByEmail(t, client.pool, "ada@example.com")

	for i := 0; i < 21; i++ {
		if _, err := client.pool.Exec(context.Background(), `
			INSERT INTO spendings (household_id, user_id, amount_minor, currency_code, entry_date, description)
			VALUES ($1, $2, 100, 'RSD', $3::date, $4)`,
			homeID, userID, fmt.Sprintf("2026-09-%02d", i+1), fmt.Sprintf("Bulk %02d", i),
		); err != nil {
			require.NoError(t, err, "insert bulk entry")
		}
	}

	response, err := client.follow.Get(client.baseURL + base + "/entries?type=spending")
	require.NoError(t, err, "GET entries page 1")
	body := readBody(t, response)
	for _, want := range []string{"Page 1 of 2", "Next", "Bulk 20"} {
		assert.Contains(t, body, want, "page 1")
	}

	response, err = client.follow.Get(client.baseURL + base + "/entries?type=spending&page=2")
	require.NoError(t, err, "GET entries page 2")
	body = readBody(t, response)
	for _, want := range []string{"Page 2 of 2", "Previous", "Bulk 00"} {
		assert.Contains(t, body, want, "page 2")
	}

	response, err = client.follow.Get(client.baseURL + base + "/entries?type=spending&from=2026-09-01&to=2026-09-01")
	require.NoError(t, err, "GET filtered entries")
	body = readBody(t, response)
	assert.Contains(t, body, "Bulk 00", "date filter")
	assert.NotContains(t, body, "Bulk 01", "date filter did not isolate one entry")
}

func (c *testClient) postFormHTMXNoRedirect(t *testing.T, path string, values url.Values) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, c.baseURL+path, strings.NewReader(values.Encode()))
	require.NoError(t, err, "create HTMX POST request")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	response, err := c.noRedirect.Do(request)
	require.NoError(t, err, "HTMX POST %s", path)
	return response
}

func (c *testClient) postFormHTMX(t *testing.T, path string, values url.Values) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, c.baseURL+path, strings.NewReader(values.Encode()))
	require.NoError(t, err, "create HTMX POST request")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	response, err := c.follow.Do(request)
	require.NoError(t, err, "HTMX POST %s", path)
	return response
}

func TestEntryHTMXFormResponses(t *testing.T) {
	client := newTestClient(t)
	csrf := client.registerHTML(t, "ada@example.com")
	homeID := createHouseholdHTML(t, client, csrf, "Home")
	base := "/app/" + homeID.String()

	response := client.postFormHTMX(t, base+"/spendings", url.Values{
		"csrf_token":    {csrf},
		"amount":        {"12.34"},
		"currency_code": {"EUR"},
		"entry_date":    {"2026-10-05"},
		"description":   {"HTMX coffee"},
	})
	assert.NoError(t, response.Body.Close(), "close response body")
	require.Equal(t, http.StatusNoContent, response.StatusCode, "HTMX create")
	redirect := response.Header.Get("HX-Redirect")
	assert.Contains(t, redirect, base+"/entries?", "HX-Redirect")
	assert.Contains(t, redirect, "notice=entry_created", "HX-Redirect")

	response = client.postFormHTMX(t, base+"/spendings", url.Values{
		"csrf_token":    {csrf},
		"amount":        {"1.234"},
		"currency_code": {"EUR"},
		"entry_date":    {"2026-10-05"},
		"description":   {"Invalid HTMX coffee"},
	})
	body := readBody(t, response)
	require.Equal(t, http.StatusOK, response.StatusCode, "HTMX validation")
	for _, want := range []string{`id="entry-form"`, "at most 2 decimal places", "Invalid HTMX coffee"} {
		assert.Contains(t, body, want, "HTMX validation body")
	}
	assert.NotContains(t, body, "<html", "HTMX validation returned a full page instead of a form fragment")

	entryID := entryIDByDescription(t, client, "spendings", "HTMX coffee")
	response = client.postFormHTMX(t, base+"/spendings/"+strconv.FormatInt(entryID, 10), url.Values{
		"csrf_token":    {csrf},
		"amount":        {"13.00"},
		"currency_code": {"EUR"},
		"entry_date":    {"2026-10-06"},
		"description":   {"HTMX lunch"},
	})
	assert.NoError(t, response.Body.Close(), "close response body")
	require.Equal(t, http.StatusNoContent, response.StatusCode, "HTMX update")
	assert.Contains(t, response.Header.Get("HX-Redirect"), "notice=entry_updated", "HX-Redirect")

	response = client.postFormHTMXNoRedirect(t, base+"/spendings/"+strconv.FormatInt(entryID, 10)+"/delete", url.Values{
		"csrf_token":  {csrf},
		"return_page": {"1"},
	})
	assert.NoError(t, response.Body.Close(), "close response body")
	require.Equal(t, http.StatusSeeOther, response.StatusCode, "HTMX delete")
	redirect = response.Header.Get("Location")
	assert.Contains(t, redirect, "notice=entry_deleted", "delete Location")
	assert.Contains(t, redirect, "type=spending", "delete Location")

	response = client.postFormHTMXNoRedirect(t, base+"/spendings/"+strconv.FormatInt(entryID, 10)+"/delete", url.Values{
		"csrf_token": {csrf},
	})
	body = readBody(t, response)
	require.Equal(t, http.StatusOK, response.StatusCode, "HTMX delete missing; body = %q", body)
	assert.Contains(t, body, `id="entries-content"`, "delete failure fragment")
	assert.Contains(t, body, "no longer available", "delete failure fragment")
	assert.NotContains(t, body, "entry_deleted", "delete failure fragment advertised success")
	assert.Contains(t, body, "Back to entries")
}
