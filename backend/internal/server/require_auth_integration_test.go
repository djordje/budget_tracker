//go:build integration

package server

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"budgettracker/internal/testdb"
)

func (c *testClient) postFormNoRedirect(t *testing.T, path string, values url.Values) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, c.baseURL+path, strings.NewReader(values.Encode()))
	require.NoError(t, err, "create POST request")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := c.noRedirect.Do(request)
	require.NoError(t, err, "POST %s", path)
	return response
}

// TestProtectedHTMLRoutesRedirectAnonymousRequests proves the RequireAuth
// wrapper is present on every de-redundified handler by driving the real route
// table. CSRF protection runs before RequireAuth, so unsafe requests carry a
// token from a public page; the assertion is the redirect, not CSRF.
func TestProtectedHTMLRoutesRedirectAnonymousRequests(t *testing.T) {
	pool := testdb.New(t)
	client := newTestClientWithPool(t, pool)
	csrf := client.csrfToken(t, "/login")

	householdID := uuid.NewString()
	invitationID := uuid.NewString()
	routes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/app"},
		{http.MethodGet, "/app/households/new"},
		{http.MethodPost, "/app/households"},
		{http.MethodGet, "/app/households/" + householdID + "/settings"},
		{http.MethodPost, "/app/households/" + householdID + "/settings"},
		{http.MethodPost, "/app/active-household"},
		{http.MethodGet, "/app/preferences"},
		{http.MethodPost, "/app/preferences"},
		{http.MethodPost, "/app/households/" + householdID + "/options"},
		{http.MethodPost, "/app/households/" + householdID + "/invitations"},
		{http.MethodPost, "/app/households/" + householdID + "/invitations/" + invitationID + "/delete"},
		{http.MethodGet, "/app/entries"},
		{http.MethodGet, "/app/exchange-rates"},
		{http.MethodPost, "/app/exchange-rates"},
		{http.MethodGet, "/app/exchange-rates/7/edit"},
		{http.MethodPost, "/app/exchange-rates/7"},
		{http.MethodPost, "/app/exchange-rates/7/delete"},
		{http.MethodGet, "/app/reports"},
		{http.MethodGet, "/app/incomes/new"},
		{http.MethodPost, "/app/incomes"},
		{http.MethodGet, "/app/incomes/42/edit"},
		{http.MethodPost, "/app/incomes/42"},
		{http.MethodPost, "/app/incomes/42/delete"},
		{http.MethodGet, "/app/spendings/new"},
		{http.MethodPost, "/app/spendings"},
		{http.MethodGet, "/app/spendings/42/edit"},
		{http.MethodPost, "/app/spendings/42"},
		{http.MethodPost, "/app/spendings/42/delete"},
		{http.MethodPost, "/logout"},
	}

	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			var response *http.Response
			if route.method == http.MethodGet {
				var err error
				response, err = client.noRedirect.Get(client.baseURL + route.path)
				require.NoError(t, err, "GET %s", route.path)
			} else {
				response = client.postFormNoRedirect(t, route.path, url.Values{"csrf_token": {csrf}})
			}
			body := readBody(t, response)

			require.Equal(t, http.StatusSeeOther, response.StatusCode, "body = %s", body)
			assert.Equal(t, "/login", response.Header.Get("Location"), "Location")
		})
	}

	for _, table := range []string{
		"users",
		"auth_credentials",
		"sessions",
		"households",
		"household_memberships",
		"household_options",
		"user_options",
		"incomes",
		"spendings",
		"exchange_rates",
		"household_invitations",
	} {
		var count int
		require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&count), "count %s", table)
		assert.Zero(t, count, "%s rows after anonymous requests", table)
	}
}
