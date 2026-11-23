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

	base := "/app/" + uuid.NewString()
	invitationID := uuid.NewString()
	routes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/app"},
		{http.MethodGet, "/app/households/new"},
		{http.MethodPost, "/app/households"},
		{http.MethodGet, base},
		{http.MethodGet, base + "/settings"},
		{http.MethodPost, base + "/settings"},
		{http.MethodPost, "/app/active-household"},
		{http.MethodGet, base + "/preferences"},
		{http.MethodPost, base + "/preferences"},
		{http.MethodPost, base + "/settings/options"},
		{http.MethodPost, base + "/settings/invitations"},
		{http.MethodPost, base + "/settings/invitations/" + invitationID + "/delete"},
		{http.MethodGet, base + "/entries"},
		{http.MethodGet, base + "/exchange-rates"},
		{http.MethodPost, base + "/exchange-rates"},
		{http.MethodGet, base + "/exchange-rates/7/edit"},
		{http.MethodPost, base + "/exchange-rates/7"},
		{http.MethodPost, base + "/exchange-rates/7/delete"},
		{http.MethodGet, base + "/reports?scope=all&currency=RSD"},
		{http.MethodGet, base + "/incomes/new"},
		{http.MethodPost, base + "/incomes"},
		{http.MethodGet, base + "/incomes/42/edit"},
		{http.MethodPost, base + "/incomes/42"},
		{http.MethodPost, base + "/incomes/42/delete"},
		{http.MethodGet, base + "/spendings/new"},
		{http.MethodPost, base + "/spendings"},
		{http.MethodGet, base + "/spendings/42/edit"},
		{http.MethodPost, base + "/spendings/42"},
		{http.MethodPost, base + "/spendings/42/delete"},
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
			want := "/login"
			if route.method == http.MethodGet {
				want += "?next=" + url.QueryEscape(route.path)
			}
			assert.Equal(t, want, response.Header.Get("Location"), "Location")
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
