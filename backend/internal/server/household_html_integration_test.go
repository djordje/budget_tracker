//go:build integration

package server

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/google/uuid"
)

func createHouseholdHTML(t *testing.T, client *testClient, csrf, name string) uuid.UUID {
	t.Helper()
	response := client.postForm(t, "/app/households", url.Values{
		"csrf_token":    {csrf},
		"name":          {name},
		"currency_code": {"RSD"},
	})
	body := readBody(t, response)
	require.Equal(t, http.StatusOK, response.StatusCode, "create household; body:\n%s", body)
	assert.Contains(t, body, "Household created.", "entries does not show creation notice")

	userID := userIDByEmail(t, client.pool, "ada@example.com")
	var householdID uuid.UUID
	if err := client.pool.QueryRow(context.Background(), `
		SELECT h.id
		FROM households h
		JOIN household_memberships m ON m.household_id = h.id
		WHERE m.user_id = $1 AND h.name = $2
		ORDER BY h.created_at DESC
		LIMIT 1`, userID, name,
	).Scan(&householdID); err != nil {
		require.NoError(t, err, "look up created household")
	}
	return householdID
}

func TestHouseholdHTMLRequiresVisibleCurrencyChoice(t *testing.T) {
	client := newTestClient(t)
	csrf := client.registerHTML(t, "ada@example.com")

	response, err := client.follow.Get(client.baseURL + "/app/households/new")
	require.NoError(t, err, "GET new household")
	body := readBody(t, response)
	require.Equal(t, http.StatusOK, response.StatusCode, "new household; body:\n%s", body)
	assert.Contains(t, body, `id="currency_code"`, "new household has no currency choice")
	assert.Contains(t, body, `value="EUR"`, "new household does not list the catalog")

	response = client.postForm(t, "/app/households", url.Values{
		"csrf_token": {csrf},
		"name":       {"Euro home"},
	})
	body = readBody(t, response)
	require.Equal(t, http.StatusUnprocessableEntity, response.StatusCode, "missing currency; body:\n%s", body)
	assert.Contains(t, body, "Choose a currency.")
	var count int
	require.NoError(t, client.pool.QueryRow(context.Background(),
		"SELECT count(*) FROM households WHERE name = $1", "Euro home",
	).Scan(&count), "count households")
	assert.Equal(t, 0, count, "household created without a currency choice")
}

// selectedHouseholdCurrency returns the value of the selected option in the
// new-household currency select, or "" when the prompt is selected.
func selectedHouseholdCurrency(t *testing.T, body string) string {
	t.Helper()
	start := strings.Index(body, `id="currency_code"`)
	require.NotEqual(t, -1, start, "currency select not rendered")
	rest := body[start:]
	end := strings.Index(rest, "</select>")
	require.NotEqual(t, -1, end, "currency select is not closed")

	selected := ""
	for _, chunk := range strings.Split(rest[:end], "<option ")[1:] {
		attributes, _, ok := strings.Cut(chunk, ">")
		require.True(t, ok, "option is not closed: %s", chunk)
		if !strings.Contains(attributes, "selected") {
			continue
		}
		valueStart := strings.Index(attributes, `value="`)
		require.NotEqual(t, -1, valueStart, "selected option has no value: %s", attributes)
		value := attributes[valueStart+len(`value="`):]
		valueEnd := strings.Index(value, `"`)
		require.NotEqual(t, -1, valueEnd, "selected option value is not closed: %s", attributes)
		require.Empty(t, selected, "more than one currency option is selected")
		selected = value[:valueEnd]
	}
	return selected
}

func TestHouseholdHTMLPreservesOnlyValidCurrencySelectionsOnError(t *testing.T) {
	client := newTestClient(t)
	csrf := client.registerHTML(t, "ada@example.com")

	for _, tc := range []struct {
		name         string
		formName     string
		currency     string
		wantSelected string
		message      string
	}{
		{
			name:     "unsupported currency",
			formName: "Home",
			currency: "XXX",
			message:  "Choose a supported currency.",
		},
		{
			name:         "valid currency with invalid name",
			currency:     " eur ",
			wantSelected: "EUR",
			message:      "Household name is required.",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := client.postForm(t, "/app/households", url.Values{
				"csrf_token":    {csrf},
				"name":          {tc.formName},
				"currency_code": {tc.currency},
			})
			body := readBody(t, response)
			require.Equal(t, http.StatusUnprocessableEntity, response.StatusCode, "body:\n%s", body)
			assert.Contains(t, body, tc.message)
			assert.Equal(t, tc.wantSelected, selectedHouseholdCurrency(t, body), "selected currency")
		})
	}
}

func TestHouseholdHTMLSeedsChosenCurrency(t *testing.T) {
	client := newTestClient(t)
	csrf := client.registerHTML(t, "ada@example.com")

	response := client.postForm(t, "/app/households", url.Values{
		"csrf_token":    {csrf},
		"name":          {"Euro home"},
		"currency_code": {"EUR"},
	})
	body := readBody(t, response)
	require.Equal(t, http.StatusOK, response.StatusCode, "create household; body:\n%s", body)
	assert.Contains(t, body, "Household created.")

	var income, spending, display string
	require.NoError(t, client.pool.QueryRow(context.Background(), `
		SELECT o.default_income_currency_code, o.default_spending_currency_code,
		       o.default_display_currency_code
		FROM household_options o
		JOIN households h ON h.id = o.household_id
		WHERE h.name = $1`, "Euro home",
	).Scan(&income, &spending, &display), "read household options")
	assert.Equal(t, "EUR", income, "default income currency")
	assert.Equal(t, "EUR", spending, "default spending currency")
	assert.Equal(t, "EUR", display, "default display currency")
}

func TestHouseholdHTMLRejectsInvalidUTF8Name(t *testing.T) {
	client := newTestClient(t)
	csrf := client.registerHTML(t, "ada@example.com")

	response := client.postForm(t, "/app/households", url.Values{
		"csrf_token":    {csrf},
		"name":          {"B\xffd"},
		"currency_code": {"RSD"},
	})
	body := readBody(t, response)

	require.Equal(t, http.StatusUnprocessableEntity, response.StatusCode, "invalid UTF-8 household; body:\n%s", body)
	assert.Contains(t, body, "Household name contains invalid characters.", "field validation message")
}

func TestHouseholdHTMLFlow(t *testing.T) {
	client := newTestClient(t)
	csrf := client.registerHTML(t, "ada@example.com")

	response := client.postForm(t, "/app/households", url.Values{
		"csrf_token":    {csrf},
		"name":          {""},
		"currency_code": {"RSD"},
	})
	body := readBody(t, response)
	require.Equal(t, http.StatusUnprocessableEntity, response.StatusCode, "invalid household; body:\n%s", body)
	assert.Contains(t, body, "Household name is required.")

	homeID := createHouseholdHTML(t, client, csrf, "Home")
	settingsPath := "/app/" + homeID.String() + "/settings"

	response, err := client.follow.Get(client.baseURL + settingsPath)
	require.NoError(t, err, "GET settings")
	body = readBody(t, response)
	require.Equal(t, http.StatusOK, response.StatusCode, "GET settings; body:\n%s", body)
	for _, want := range []string{"Home", "Members", "ada@example.com", "RSD"} {
		assert.Contains(t, body, want, "settings")
	}

	response = client.postForm(t, settingsPath, url.Values{
		"csrf_token": {csrf},
		"name":       {"Shared home"},
	})
	body = readBody(t, response)
	require.Equal(t, http.StatusOK, response.StatusCode, "rename household; body:\n%s", body)
	assert.Contains(t, body, "Household settings saved.")
	assert.Contains(t, body, "Shared home")

	grace := newTestClientWithPool(t, client.pool)
	grace.registerHTML(t, "grace@example.com")

	response, err = grace.follow.Get(grace.baseURL + settingsPath)
	require.NoError(t, err, "grace GET settings")
	assert.NoError(t, response.Body.Close(), "close response body")
	assert.Equal(t, http.StatusNotFound, response.StatusCode, "non-member settings")

	graceID := userIDByEmail(t, client.pool, "grace@example.com")
	if _, err := client.pool.Exec(context.Background(), `
		INSERT INTO household_memberships (household_id, user_id, role)
		VALUES ($1, $2, 'member')`, homeID, graceID,
	); err != nil {
		require.NoError(t, err, "add member")
	}

	response, err = grace.follow.Get(grace.baseURL + settingsPath)
	require.NoError(t, err, "member GET settings")
	body = readBody(t, response)
	assert.Equal(t, http.StatusForbidden, response.StatusCode, "member settings; body:\n%s", body)
	assert.Contains(t, body, "Only household admins")
	assert.NotContains(t, grace.visitHouseholdEntries(t, homeID), `href="`+settingsPath+`"`)

	response, err = client.follow.Get(client.baseURL + settingsPath)
	require.NoError(t, err, "admin GET settings")
	body = readBody(t, response)
	assert.Contains(t, body, "grace@example.com", "admin settings does not list member")

	createHouseholdHTML(t, client, csrf, "Second home")
	response, err = client.follow.Get(client.baseURL + "/app")
	require.NoError(t, err, "GET entries after second household")
	body = readBody(t, response)
	assert.Contains(t, body, "Second home", "second household was not made active")

	assert.Contains(t, body, `href="/app/`+homeID.String()+`/entries">Shared home</a>`)
	body = client.visitHouseholdEntries(t, homeID)
	assert.Contains(t, body, `aria-label="Households: Shared home"`)
}

func (c *testClient) visitHouseholdEntries(t *testing.T, id uuid.UUID) string {
	t.Helper()
	path := "/app/" + id.String() + "/entries"
	response, err := c.noRedirect.Get(c.baseURL + path)
	require.NoError(t, err)
	body := readBody(t, response)
	require.Equal(t, http.StatusOK, response.StatusCode, "GET %s; body:\n%s", path, body)
	return body
}

func TestHouseholdHTMLRequiresAuth(t *testing.T) {
	client := newTestClient(t)
	response, err := client.noRedirect.Get(client.baseURL + "/app/households/new")
	require.NoError(t, err, "GET new household")
	assert.NoError(t, response.Body.Close(), "close response body")
	assert.Equal(t, http.StatusSeeOther, response.StatusCode, "GET new household")
	assert.Equal(t, "/login?next="+url.QueryEscape("/app/households/new"), response.Header.Get("Location"), "Location")
}
