//go:build integration

package server

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHouseholdHTMLLandingAndLoginDeepLink(t *testing.T) {
	client := newTestClient(t)
	csrf := client.registerHTML(t, "ada@example.com")
	response, err := client.noRedirect.Get(client.baseURL + "/app")
	require.NoError(t, err)
	readBody(t, response)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	assert.Equal(t, "/app/households/new", response.Header.Get("Location"))

	id := createHouseholdHTML(t, client, csrf, "Deep link home")
	base := "/app/" + id.String()
	for _, hop := range []struct{ from, to string }{{"/app", base}, {base, base + "/entries"}} {
		response, err := client.noRedirect.Get(client.baseURL + hop.from)
		require.NoError(t, err)
		readBody(t, response)
		require.Equal(t, http.StatusSeeOther, response.StatusCode)
		assert.Equal(t, hop.to, response.Header.Get("Location"))
	}

	readBody(t, client.postForm(t, "/logout", url.Values{"csrf_token": {csrf}}))
	path := base + "/reports?from=2026-10-01&to=2026-10-31&currency=EUR"
	response, err = client.noRedirect.Get(client.baseURL + path)
	require.NoError(t, err)
	readBody(t, response)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	login := response.Header.Get("Location")
	assert.Equal(t, "/login?next="+url.QueryEscape(path), login)
	csrf = client.csrfToken(t, login)
	response = client.postForm(t, "/login", url.Values{
		"csrf_token": {csrf}, "email": {"ada@example.com"}, "password": {"correct horse battery staple"}, "next": {path},
	})
	body := readBody(t, response)
	require.Equal(t, http.StatusOK, response.StatusCode)
	assert.Equal(t, path, response.Request.URL.RequestURI())
	assert.Contains(t, body, `name="from" type="date" value="2026-10-01"`)
	assert.Contains(t, body, `hx-get="`+base+`/reports"`)
	assert.Contains(t, body, `hx-push-url="true"`)
}

func TestScopedHTMLKeepsTwoTabsIndependent(t *testing.T) {
	client := newTestClient(t)
	csrf := client.registerHTML(t, "ada@example.com")
	a := createHouseholdHTML(t, client, csrf, "First home")
	b := createHouseholdHTML(t, client, csrf, "Second home")
	baseA, baseB := "/app/"+a.String(), "/app/"+b.String()
	for _, kind := range []string{"spendings", "incomes"} {
		t.Run(kind, func(t *testing.T) {
			action := baseA + "/" + kind
			response, err := client.follow.Get(client.baseURL + action + "/new")
			require.NoError(t, err)
			body := readBody(t, response)
			require.Equal(t, http.StatusOK, response.StatusCode)
			assert.Contains(t, body, `action="`+action+`"`)
			assert.Contains(t, body, `hx-post="`+action+`"`)

			response, err = client.follow.Get(client.baseURL + baseB + "/entries")
			require.NoError(t, err)
			readBody(t, response)
			require.Equal(t, http.StatusOK, response.StatusCode)
			response, err = client.noRedirect.Get(client.baseURL + "/app")
			require.NoError(t, err)
			readBody(t, response)
			require.Equal(t, http.StatusSeeOther, response.StatusCode)
			assert.Equal(t, baseB, response.Header.Get("Location"))

			form := url.Values{
				"csrf_token": {csrf}, "amount": {"1.001"}, "currency_code": {"EUR"}, "entry_date": {"2026-10-05"},
				"description": {"Entry from tab A"}, "household_id": {b.String()},
			}
			post := client.postFormNoRedirect
			invalid, success, redirect := http.StatusUnprocessableEntity, http.StatusSeeOther, "Location"
			if kind == "incomes" {
				post = client.postFormHTMXNoRedirect
				invalid, success, redirect = http.StatusOK, http.StatusNoContent, "HX-Redirect"
			}
			response = post(t, action, form)
			body = readBody(t, response)
			require.Equal(t, invalid, response.StatusCode)
			assert.Contains(t, body, "at most 2 decimal places")
			assert.Contains(t, body, `action="`+action+`"`)
			form.Set("amount", "12.34")
			response = post(t, action, form)
			readBody(t, response)
			require.Equal(t, success, response.StatusCode)
			assert.Contains(t, response.Header.Get(redirect), baseA+"/entries?")

			id := entryIDByDescription(t, client, kind, "Entry from tab A")
			wrong := baseB + "/" + kind + "/" + strconv.FormatInt(id, 10)
			response, err = client.noRedirect.Get(client.baseURL + wrong + "/edit")
			require.NoError(t, err)
			readBody(t, response)
			assert.Equal(t, http.StatusNotFound, response.StatusCode)
			form.Set("amount", "99")
			for _, path := range []string{wrong, wrong + "/delete"} {
				response := client.postFormNoRedirect(t, path, form)
				readBody(t, response)
				assert.Equal(t, http.StatusNotFound, response.StatusCode)
			}
			var stored uuid.UUID
			var amount int64
			require.NoError(t, client.pool.QueryRow(t.Context(), "SELECT household_id, amount_minor FROM "+kind+" WHERE id = $1", id).Scan(&stored, &amount))
			assert.Equal(t, a, stored)
			assert.EqualValues(t, 1234, amount)
		})
	}

	// Rate IDs, like entry IDs, cannot address another household through the current URL.
	response := client.postFormNoRedirect(t, baseA+"/exchange-rates", url.Values{
		"csrf_token": {csrf}, "from_currency_code": {"EUR"}, "to_currency_code": {"RSD"}, "rate": {"117"}, "rate_date": {"2026-10-05"},
	})
	readBody(t, response)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	wrongRate := baseB + "/exchange-rates/" + strconv.FormatInt(rateIDByPair(t, client, "EUR", "RSD", false), 10)
	response, err := client.noRedirect.Get(client.baseURL + wrongRate + "/edit")
	require.NoError(t, err)
	readBody(t, response)
	assert.Equal(t, http.StatusNotFound, response.StatusCode)
	for _, path := range []string{wrongRate, wrongRate + "/delete"} {
		response := client.postFormNoRedirect(t, path, url.Values{"csrf_token": {csrf}})
		readBody(t, response)
		assert.Equal(t, http.StatusNotFound, response.StatusCode)
	}
}

func TestHouseholdNavigationFollowsMatchedPage(t *testing.T) {
	client := newTestClient(t)
	csrf := client.registerHTML(t, "ada@example.com")
	baseA := "/app/" + createHouseholdHTML(t, client, csrf, "First home").String()
	baseB := "/app/" + createHouseholdHTML(t, client, csrf, "Second home").String()
	for _, tc := range []struct {
		path, switchTo, current string
	}{
		{"/entries?type=income", "/entries", "/entries"},
		{"/reports?currency=EUR", "/reports", "/reports"},
		{"/spendings/new?section=reports", "/entries", ""},
		{"/preferences", "/entries", "/preferences"},
		{"/exchange-rates", "/exchange-rates", "/exchange-rates"},
		{"/settings", "/entries", "/settings"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			response, err := client.noRedirect.Get(client.baseURL + baseA + tc.path)
			require.NoError(t, err)
			body := readBody(t, response)
			require.Equal(t, http.StatusOK, response.StatusCode)
			assert.Contains(t, body, `aria-label="Households: First home"`)
			assert.Contains(t, body, `href="`+baseB+tc.switchTo+`">Second home</a>`)
			assert.Contains(t, body, `href="/app/households/new">Create household</a>`)
			assert.Contains(t, body, `<details name="header-menu"`)
			assert.Contains(t, body, `<span class="sr-only">Menu</span>`)
			if tc.current == "" {
				assert.Contains(t, body, `class="menu-item menu-active" href="`+baseA+`/entries">Entries</a>`)
				assert.NotContains(t, body, `aria-current="page"`)
			} else {
				assert.Contains(t, body, `href="`+baseA+tc.current+`" aria-current="page"`)
			}
		})
	}
}

func TestRemovedHTMLRoutesDoNotFallBack(t *testing.T) {
	client := newTestClient(t)
	csrf := client.registerHTML(t, "ada@example.com")
	id := createHouseholdHTML(t, client, csrf, "Home")
	for _, path := range []string{
		"/app/entries", "/app/reports", "/app/preferences", "/app/incomes/new", "/app/spendings/new",
		"/app/exchange-rates", "/app/households/" + id.String() + "/settings",
	} {
		response, err := client.noRedirect.Get(client.baseURL + path)
		require.NoError(t, err)
		readBody(t, response)
		assert.Equal(t, http.StatusNotFound, response.StatusCode, path)
		assert.Empty(t, response.Header.Get("Location"), path)
	}
	for _, path := range []string{
		"/app/preferences", "/app/incomes", "/app/spendings", "/app/exchange-rates", "/app/spendings/42/delete", "/app/active-household",
		"/app/households/" + id.String() + "/options",
	} {
		response := client.postFormNoRedirect(t, path, url.Values{"csrf_token": {csrf}})
		readBody(t, response)
		assert.Contains(t, []int{http.StatusNotFound, http.StatusMethodNotAllowed}, response.StatusCode, path)
		assert.Empty(t, response.Header.Get("Location"), path)
	}
}
