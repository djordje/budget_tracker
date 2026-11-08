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
)

func entryIDByDescription(t *testing.T, client *testClient, table, description string) int64 {
	t.Helper()
	var id int64
	if err := client.pool.QueryRow(context.Background(), fmt.Sprintf(`
		SELECT id FROM %s WHERE description = $1 ORDER BY created_at DESC LIMIT 1`, table),
		description,
	).Scan(&id); err != nil {
		t.Fatalf("look up %s entry %q: %v", table, description, err)
	}
	return id
}

func TestEntryHTMLFlow(t *testing.T) {
	client := newTestClient(t)
	csrf := client.registerHTML(t, "ada@example.com")
	homeID := createHouseholdHTML(t, client, csrf, "Home")

	response, err := client.follow.Get(client.baseURL + "/app/entries")
	if err != nil {
		t.Fatalf("GET entries: %v", err)
	}
	body := readBody(t, response)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "No Spending entries yet") {
		t.Fatalf("empty entries = %d, body:\n%s", response.StatusCode, body)
	}

	response, err = client.follow.Get(client.baseURL + "/app/spendings/new")
	if err != nil {
		t.Fatalf("GET spending form: %v", err)
	}
	body = readBody(t, response)
	for _, want := range []string{"New spending", `name="amount"`, "RSD", "Whole household"} {
		if !strings.Contains(body, want) {
			t.Errorf("spending form does not contain %q", want)
		}
	}

	response = client.postForm(t, "/app/spendings", url.Values{
		"csrf_token":    {csrf},
		"amount":        {"12.34"},
		"currency_code": {"EUR"},
		"entry_date":    {"2026-10-05"},
		"description":   {"Coffee"},
		"attribution":   {"self"},
	})
	body = readBody(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("create spending = %d, body:\n%s", response.StatusCode, body)
	}
	for _, want := range []string{"Entry added.", "Coffee", "-12.34 EUR"} {
		if !strings.Contains(body, want) {
			t.Errorf("entries list does not contain %q", want)
		}
	}

	spendingID := entryIDByDescription(t, client, "spendings", "Coffee")
	response, err = client.follow.Get(client.baseURL + "/app/spendings/" + strconv.FormatInt(spendingID, 10) + "/edit")
	if err != nil {
		t.Fatalf("GET edit spending: %v", err)
	}
	body = readBody(t, response)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "Coffee") || !strings.Contains(body, `value="12.34"`) {
		t.Fatalf("edit spending = %d, body:\n%s", response.StatusCode, body)
	}

	response = client.postForm(t, "/app/spendings/"+strconv.FormatInt(spendingID, 10), url.Values{
		"csrf_token":    {csrf},
		"amount":        {"15.00"},
		"currency_code": {"EUR"},
		"entry_date":    {"2026-10-06"},
		"description":   {"Lunch"},
	})
	body = readBody(t, response)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "Entry updated.") || !strings.Contains(body, "Lunch") {
		t.Fatalf("update spending = %d, body:\n%s", response.StatusCode, body)
	}

	response = client.postForm(t, "/app/spendings/"+strconv.FormatInt(spendingID, 10)+"/delete", url.Values{"csrf_token": {csrf}})
	body = readBody(t, response)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "Entry deleted.") || strings.Contains(body, "Lunch") {
		t.Fatalf("delete spending = %d, body:\n%s", response.StatusCode, body)
	}

	response = client.postForm(t, "/app/incomes", url.Values{
		"csrf_token":    {csrf},
		"amount":        {"100"},
		"currency_code": {"RSD"},
		"entry_date":    {"2026-10-01"},
		"description":   {"Salary"},
	})
	readBody(t, response)

	response, err = client.follow.Get(client.baseURL + "/app/entries?type=income")
	if err != nil {
		t.Fatalf("GET income entries: %v", err)
	}
	body = readBody(t, response)
	if !strings.Contains(body, "Salary") || !strings.Contains(body, "+100.00 RSD") {
		t.Errorf("income list missing entry:\n%s", body)
	}

	// Admin-only household-wide entry.
	response = client.postForm(t, "/app/incomes", url.Values{
		"csrf_token":    {csrf},
		"amount":        {"500"},
		"currency_code": {"RSD"},
		"entry_date":    {"2026-10-02"},
		"description":   {"Rent"},
		"attribution":   {"household"},
	})
	readBody(t, response)
	response, err = client.follow.Get(client.baseURL + "/app/entries?type=income&scope=all")
	if err != nil {
		t.Fatalf("GET all income entries: %v", err)
	}
	body = readBody(t, response)
	if !strings.Contains(body, "Household") || !strings.Contains(body, "Rent") {
		t.Errorf("admin all list missing household entry:\n%s", body)
	}

	member := newTestClientWithPool(t, client.pool)
	memberCSRF := member.registerHTML(t, "grace@example.com")
	graceID := userIDByEmail(t, client.pool, "grace@example.com")
	if _, err := client.pool.Exec(context.Background(), `
		INSERT INTO household_memberships (household_id, user_id, role)
		VALUES ($1, $2, 'member')`, homeID, graceID,
	); err != nil {
		t.Fatalf("add member: %v", err)
	}
	response = member.postForm(t, "/app/active-household", url.Values{
		"csrf_token":   {memberCSRF},
		"household_id": {homeID.String()},
	})
	readBody(t, response)

	response, err = member.follow.Get(member.baseURL + "/app/spendings/new")
	if err != nil {
		t.Fatalf("member GET spending form: %v", err)
	}
	body = readBody(t, response)
	if strings.Contains(body, "Whole household") || !strings.Contains(body, "Your entry") {
		t.Errorf("member spending form attribution:\n%s", body)
	}

	response = member.postForm(t, "/app/spendings", url.Values{
		"csrf_token":    {memberCSRF},
		"amount":        {"9.99"},
		"currency_code": {"EUR"},
		"entry_date":    {"2026-10-07"},
		"description":   {"Member coffee"},
		"attribution":   {"household"},
	})
	body = readBody(t, response)
	if response.StatusCode != http.StatusForbidden || !strings.Contains(body, "do not have permission") {
		t.Fatalf("member household-wide create = %d, body:\n%s", response.StatusCode, body)
	}

	response = member.postForm(t, "/app/spendings", url.Values{
		"csrf_token":    {memberCSRF},
		"amount":        {"9.99"},
		"currency_code": {"EUR"},
		"entry_date":    {"2026-10-07"},
		"description":   {"Member coffee"},
	})
	readBody(t, response)
	memberSpendingID := entryIDByDescription(t, member, "spendings", "Member coffee")

	response, err = client.follow.Get(client.baseURL + "/app/spendings/" + strconv.FormatInt(memberSpendingID, 10) + "/edit")
	if err != nil {
		t.Fatalf("admin GET member entry edit: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Errorf("admin edit member entry = %d, want 403", response.StatusCode)
	}

	response, err = member.follow.Get(member.baseURL + "/app/incomes/" + strconv.FormatInt(entryIDByDescription(t, client, "incomes", "Salary"), 10) + "/edit")
	if err != nil {
		t.Fatalf("member GET admin entry edit: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Errorf("member edit admin entry = %d, want 404", response.StatusCode)
	}

	// Validation preserves values and reports the amount error.
	response = member.postForm(t, "/app/spendings", url.Values{
		"csrf_token":    {memberCSRF},
		"amount":        {"1.234"},
		"currency_code": {"EUR"},
		"entry_date":    {"2026-10-07"},
		"description":   {"Too precise"},
	})
	body = readBody(t, response)
	if response.StatusCode != http.StatusUnprocessableEntity ||
		!strings.Contains(body, "at most 2 decimal places") ||
		!strings.Contains(body, "Too precise") {
		t.Fatalf("invalid amount = %d, body:\n%s", response.StatusCode, body)
	}
}

func TestEntryHTMLRequiresHouseholdAndCSRF(t *testing.T) {
	client := newTestClient(t)
	csrf := client.registerHTML(t, "ada@example.com")

	response, err := client.noRedirect.Get(client.baseURL + "/app/spendings/new")
	if err != nil {
		t.Fatalf("GET spending form without household: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusSeeOther || response.Header.Get("Location") != "/app" {
		t.Errorf("no-household form = %d %q, want 303 /app", response.StatusCode, response.Header.Get("Location"))
	}

	createHouseholdHTML(t, client, csrf, "Home")
	response = client.postForm(t, "/app/spendings", url.Values{
		"amount": {"1"}, "currency_code": {"RSD"}, "entry_date": {"2026-10-01"},
	})
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Errorf("missing CSRF = %d, want 403", response.StatusCode)
	}
}

func TestEntryHTMLPaginationAndFilters(t *testing.T) {
	client := newTestClient(t)
	csrf := client.registerHTML(t, "ada@example.com")
	homeID := createHouseholdHTML(t, client, csrf, "Home")
	userID := userIDByEmail(t, client.pool, "ada@example.com")

	for i := 0; i < 21; i++ {
		if _, err := client.pool.Exec(context.Background(), `
			INSERT INTO spendings (household_id, user_id, amount_minor, currency_code, entry_date, description)
			VALUES ($1, $2, 100, 'RSD', $3::date, $4)`,
			homeID, userID, fmt.Sprintf("2026-09-%02d", i+1), fmt.Sprintf("Bulk %02d", i),
		); err != nil {
			t.Fatalf("insert bulk entry: %v", err)
		}
	}

	response, err := client.follow.Get(client.baseURL + "/app/entries?type=spending")
	if err != nil {
		t.Fatalf("GET entries page 1: %v", err)
	}
	body := readBody(t, response)
	for _, want := range []string{"Page 1 of 2", "Next", "Bulk 20"} {
		if !strings.Contains(body, want) {
			t.Errorf("page 1 does not contain %q", want)
		}
	}

	response, err = client.follow.Get(client.baseURL + "/app/entries?type=spending&page=2")
	if err != nil {
		t.Fatalf("GET entries page 2: %v", err)
	}
	body = readBody(t, response)
	for _, want := range []string{"Page 2 of 2", "Previous", "Bulk 00"} {
		if !strings.Contains(body, want) {
			t.Errorf("page 2 does not contain %q", want)
		}
	}

	response, err = client.follow.Get(client.baseURL + "/app/entries?type=spending&from=2026-09-01&to=2026-09-01")
	if err != nil {
		t.Fatalf("GET filtered entries: %v", err)
	}
	body = readBody(t, response)
	if !strings.Contains(body, "Bulk 00") || strings.Contains(body, "Bulk 01") {
		t.Errorf("date filter did not isolate one entry:\n%s", body)
	}
}

func (c *testClient) postFormHTMXNoRedirect(t *testing.T, path string, values url.Values) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, c.baseURL+path, strings.NewReader(values.Encode()))
	if err != nil {
		t.Fatalf("create HTMX POST request: %v", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	response, err := c.noRedirect.Do(request)
	if err != nil {
		t.Fatalf("HTMX POST %s: %v", path, err)
	}
	return response
}

func (c *testClient) postFormHTMX(t *testing.T, path string, values url.Values) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, c.baseURL+path, strings.NewReader(values.Encode()))
	if err != nil {
		t.Fatalf("create HTMX POST request: %v", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	response, err := c.follow.Do(request)
	if err != nil {
		t.Fatalf("HTMX POST %s: %v", path, err)
	}
	return response
}

func TestEntryHTMXFormResponses(t *testing.T) {
	client := newTestClient(t)
	csrf := client.registerHTML(t, "ada@example.com")
	createHouseholdHTML(t, client, csrf, "Home")

	response := client.postFormHTMX(t, "/app/spendings", url.Values{
		"csrf_token":    {csrf},
		"amount":        {"12.34"},
		"currency_code": {"EUR"},
		"entry_date":    {"2026-10-05"},
		"description":   {"HTMX coffee"},
	})
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("HTMX create = %d, want 204", response.StatusCode)
	}
	if redirect := response.Header.Get("HX-Redirect"); !strings.Contains(redirect, "/app/entries?") || !strings.Contains(redirect, "notice=entry_created") {
		t.Errorf("HX-Redirect = %q", redirect)
	}

	response = client.postFormHTMX(t, "/app/spendings", url.Values{
		"csrf_token":    {csrf},
		"amount":        {"1.234"},
		"currency_code": {"EUR"},
		"entry_date":    {"2026-10-05"},
		"description":   {"Invalid HTMX coffee"},
	})
	body := readBody(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("HTMX validation = %d, want 200", response.StatusCode)
	}
	for _, want := range []string{`id="entry-form"`, "at most 2 decimal places", "Invalid HTMX coffee"} {
		if !strings.Contains(body, want) {
			t.Errorf("HTMX validation body does not contain %q", want)
		}
	}
	if strings.Contains(body, "<html") {
		t.Error("HTMX validation returned a full page instead of a form fragment")
	}

	entryID := entryIDByDescription(t, client, "spendings", "HTMX coffee")
	response = client.postFormHTMX(t, "/app/spendings/"+strconv.FormatInt(entryID, 10), url.Values{
		"csrf_token":    {csrf},
		"amount":        {"13.00"},
		"currency_code": {"EUR"},
		"entry_date":    {"2026-10-06"},
		"description":   {"HTMX lunch"},
	})
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("HTMX update = %d, want 204", response.StatusCode)
	}
	if redirect := response.Header.Get("HX-Redirect"); !strings.Contains(redirect, "notice=entry_updated") {
		t.Errorf("HX-Redirect update = %q", redirect)
	}

	response = client.postFormHTMXNoRedirect(t, "/app/spendings/"+strconv.FormatInt(entryID, 10)+"/delete", url.Values{
		"csrf_token":  {csrf},
		"return_page": {"1"},
	})
	response.Body.Close()
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("HTMX delete = %d, want %d", response.StatusCode, http.StatusSeeOther)
	}
	if redirect := response.Header.Get("Location"); !strings.Contains(redirect, "notice=entry_deleted") ||
		!strings.Contains(redirect, "type=spending") {
		t.Errorf("delete Location = %q", redirect)
	}

	response = client.postFormHTMXNoRedirect(t, "/app/spendings/"+strconv.FormatInt(entryID, 10)+"/delete", url.Values{
		"csrf_token": {csrf},
	})
	body = readBody(t, response)
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("HTMX delete missing = %d, want %d", response.StatusCode, http.StatusNotFound)
	}
	if !strings.Contains(body, `id="entries-content"`) || !strings.Contains(body, "no longer available") {
		t.Errorf("delete failure fragment = %q", body)
	}
	if strings.Contains(body, "entry_deleted") || strings.Contains(body, "Back to entries") == false {
		t.Errorf("delete failure fragment advertised success: %q", body)
	}
}
