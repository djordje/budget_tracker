//go:build integration

package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jackc/pgx/v5/pgxpool"

	"budgettracker/internal/httpapi"
	"budgettracker/internal/testdb"
)

type testClient struct {
	baseURL    string
	server     *httptest.Server
	pool       *pgxpool.Pool
	jar        http.CookieJar
	follow     *http.Client
	noRedirect *http.Client
}

func newTestClient(t *testing.T) *testClient {
	t.Helper()
	return newTestClientWithPool(t, testdb.New(t))
}

func newTestClientWithPool(t *testing.T, pool *pgxpool.Pool) *testClient {
	t.Helper()
	handler, err := New(Deps{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		DB:     pool,
	})
	require.NoError(t, err, "New()")
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	jar, err := cookiejar.New(nil)
	require.NoError(t, err, "create cookie jar")

	return &testClient{
		baseURL: server.URL,
		server:  server,
		pool:    pool,
		jar:     jar,
		follow: &http.Client{
			Jar: jar,
		},
		noRedirect: &http.Client{
			Jar: jar,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (c *testClient) csrfToken(t *testing.T, path string) string {
	t.Helper()
	response, err := c.follow.Get(c.baseURL + path)
	require.NoError(t, err, "GET %s", path)
	assert.NoError(t, response.Body.Close(), "close response body")

	baseURL, err := url.Parse(c.baseURL)
	require.NoError(t, err, "parse server URL")
	for _, cookie := range c.jar.Cookies(baseURL) {
		if cookie.Name == httpapi.DefaultCSRFCookieName {
			return cookie.Value
		}
	}
	require.FailNow(t, "CSRF cookie was not set")
	return ""
}

func (c *testClient) sessionCookie(t *testing.T) *http.Cookie {
	t.Helper()
	baseURL, err := url.Parse(c.baseURL)
	require.NoError(t, err, "parse server URL")
	for _, cookie := range c.jar.Cookies(baseURL) {
		if cookie.Name == "bt_session" {
			return cookie
		}
	}
	return nil
}

func (c *testClient) postForm(t *testing.T, path string, values url.Values) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, c.baseURL+path, strings.NewReader(values.Encode()))
	require.NoError(t, err, "create POST request")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := c.follow.Do(request)
	require.NoError(t, err, "POST %s", path)
	return response
}

func readBody(t *testing.T, response *http.Response) string {
	t.Helper()
	defer func() {
		assert.NoError(t, response.Body.Close(), "close response body")
	}()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err, "read response body")
	return string(body)
}

func registerForm(csrf string) url.Values {
	return url.Values{
		"csrf_token":       {csrf},
		"first_name":       {"Ada"},
		"last_name":        {"Lovelace"},
		"email":            {"ada@example.com"},
		"password":         {"correct horse battery staple"},
		"password_confirm": {"correct horse battery staple"},
	}
}

func TestRegisterLoginAndLogoutFlow(t *testing.T) {
	client := newTestClient(t)

	response, err := client.noRedirect.Get(client.baseURL + "/app")
	require.NoError(t, err, "GET /app")
	assert.NoError(t, response.Body.Close(), "close response body")
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	assert.Equal(t, "/login", response.Header.Get("Location"), "Location")

	csrf := client.csrfToken(t, "/register")
	response = client.postForm(t, "/register", registerForm(csrf))
	body := readBody(t, response)
	require.Equal(t, http.StatusOK, response.StatusCode, "body:\n%s", body)
	for _, want := range []string{"Welcome, Ada.", "Create household"} {
		assert.Contains(t, body, want, "dashboard")
	}
	assert.NotNil(t, client.sessionCookie(t), "registration did not set a session cookie")

	response = client.postForm(t, "/logout", url.Values{"csrf_token": {csrf}})
	body = readBody(t, response)
	require.Equal(t, http.StatusOK, response.StatusCode, "body:\n%s", body)
	assert.Contains(t, body, "You have been signed out.")
	assert.Nil(t, client.sessionCookie(t), "logout did not clear the session cookie")

	response, err = client.noRedirect.Get(client.baseURL + "/app")
	require.NoError(t, err, "GET /app after logout")
	assert.NoError(t, response.Body.Close(), "close response body")
	require.Equal(t, http.StatusSeeOther, response.StatusCode, "GET /app after logout")
	assert.Equal(t, "/login", response.Header.Get("Location"), "Location")

	csrf = client.csrfToken(t, "/login")
	response = client.postForm(t, "/login", url.Values{
		"csrf_token": {csrf},
		"email":      {"ada@example.com"},
		"password":   {"wrong password"},
	})
	body = readBody(t, response)
	require.Equal(t, http.StatusUnprocessableEntity, response.StatusCode, "body:\n%s", body)
	assert.Contains(t, body, "Email or password is incorrect.")

	response = client.postForm(t, "/login", url.Values{
		"csrf_token": {csrf},
		"email":      {"  ADA@Example.COM "},
		"password":   {"correct horse battery staple"},
	})
	body = readBody(t, response)
	require.Equal(t, http.StatusOK, response.StatusCode, "body:\n%s", body)
	assert.Contains(t, body, "Welcome, Ada.")
	assert.NotNil(t, client.sessionCookie(t), "login did not set a session cookie")
}

func TestRegistrationValidationAndDuplicateEmail(t *testing.T) {
	client := newTestClient(t)
	csrf := client.csrfToken(t, "/register")

	values := registerForm(csrf)
	values.Set("password_confirm", "different password")
	response := client.postForm(t, "/register", values)
	body := readBody(t, response)
	require.Equal(t, http.StatusUnprocessableEntity, response.StatusCode, "body:\n%s", body)
	assert.Contains(t, body, "Passwords do not match.")
	assert.Nil(t, client.sessionCookie(t), "failed registration created a session")

	values = registerForm(csrf)
	response = client.postForm(t, "/register", values)
	readBody(t, response)

	response = client.postForm(t, "/logout", url.Values{"csrf_token": {csrf}})
	readBody(t, response)

	response = client.postForm(t, "/register", registerForm(csrf))
	body = readBody(t, response)
	require.Equal(t, http.StatusUnprocessableEntity, response.StatusCode, "body:\n%s", body)
	assert.Contains(t, body, "already exists")
}

func TestCSRFIsRequired(t *testing.T) {
	client := newTestClient(t)

	response := client.postForm(t, "/login", url.Values{
		"email":    {"ada@example.com"},
		"password": {"correct horse battery staple"},
	})
	body := readBody(t, response)
	require.Equal(t, http.StatusForbidden, response.StatusCode, "body:\n%s", body)
}

func TestAuthenticatedVisitorsAreRedirectedFromAuthPages(t *testing.T) {
	client := newTestClient(t)
	csrf := client.csrfToken(t, "/register")
	response := client.postForm(t, "/register", registerForm(csrf))
	readBody(t, response)

	response, err := client.noRedirect.Get(client.baseURL + "/login")
	require.NoError(t, err, "GET /login")
	assert.NoError(t, response.Body.Close(), "close response body")
	assert.Equal(t, http.StatusSeeOther, response.StatusCode, "GET /login")
	assert.Equal(t, "/app", response.Header.Get("Location"), "Location")
}

func TestLoginThrottle(t *testing.T) {
	client := newTestClient(t)
	csrf := client.registerHTML(t, "throttle@example.com")
	response := client.postForm(t, "/logout", url.Values{"csrf_token": {csrf}})
	readBody(t, response)
	csrf = client.csrfToken(t, "/login")

	for i := 0; i < 5; i++ {
		response = client.postForm(t, "/login", url.Values{
			"csrf_token": {csrf},
			"email":      {"throttle@example.com"},
			"password":   {"wrong password"},
		})
		body := readBody(t, response)
		require.Equal(t, http.StatusUnprocessableEntity, response.StatusCode, "attempt %d, body:\n%s", i+1, body)
	}
	response = client.postForm(t, "/login", url.Values{
		"csrf_token": {csrf},
		"email":      {"throttle@example.com"},
		"password":   {"correct horse battery staple"},
	})
	body := readBody(t, response)
	require.Equal(t, http.StatusTooManyRequests, response.StatusCode, "body:\n%s", body)
	assert.Contains(t, body, "Too many sign-in attempts")
	assert.Equal(t, "900", response.Header.Get("Retry-After"), "Retry-After")
}
