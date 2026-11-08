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
	pool := testdb.New(t)
	handler, err := New(Deps{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		DB:     pool,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}

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
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	response.Body.Close()

	baseURL, err := url.Parse(c.baseURL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	for _, cookie := range c.jar.Cookies(baseURL) {
		if cookie.Name == httpapi.DefaultCSRFCookieName {
			return cookie.Value
		}
	}
	t.Fatal("CSRF cookie was not set")
	return ""
}

func (c *testClient) sessionCookie(t *testing.T) *http.Cookie {
	t.Helper()
	baseURL, err := url.Parse(c.baseURL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
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
	if err != nil {
		t.Fatalf("create POST request: %v", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := c.follow.Do(request)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return response
}

func readBody(t *testing.T, response *http.Response) string {
	t.Helper()
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
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
	if err != nil {
		t.Fatalf("GET /app: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusSeeOther || response.Header.Get("Location") != "/login" {
		t.Fatalf("GET /app = %d %q, want 303 /login", response.StatusCode, response.Header.Get("Location"))
	}

	csrf := client.csrfToken(t, "/register")
	response = client.postForm(t, "/register", registerForm(csrf))
	body := readBody(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("POST /register status = %d, body:\n%s", response.StatusCode, body)
	}
	for _, want := range []string{"Welcome, Ada.", "ada@example.com"} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard does not contain %q", want)
		}
	}
	if client.sessionCookie(t) == nil {
		t.Fatal("registration did not set a session cookie")
	}

	response = client.postForm(t, "/logout", url.Values{"csrf_token": {csrf}})
	body = readBody(t, response)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "You have been signed out.") {
		t.Fatalf("POST /logout status = %d, body:\n%s", response.StatusCode, body)
	}
	if client.sessionCookie(t) != nil {
		t.Fatal("logout did not clear the session cookie")
	}

	response, err = client.noRedirect.Get(client.baseURL + "/app")
	if err != nil {
		t.Fatalf("GET /app after logout: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusSeeOther || response.Header.Get("Location") != "/login" {
		t.Fatalf("GET /app after logout = %d %q, want 303 /login", response.StatusCode, response.Header.Get("Location"))
	}

	csrf = client.csrfToken(t, "/login")
	response = client.postForm(t, "/login", url.Values{
		"csrf_token": {csrf},
		"email":      {"ada@example.com"},
		"password":   {"wrong password"},
	})
	body = readBody(t, response)
	if response.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "Email or password is incorrect.") {
		t.Fatalf("POST /login wrong password = %d, body:\n%s", response.StatusCode, body)
	}

	response = client.postForm(t, "/login", url.Values{
		"csrf_token": {csrf},
		"email":      {"  ADA@Example.COM "},
		"password":   {"correct horse battery staple"},
	})
	body = readBody(t, response)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "Welcome, Ada.") {
		t.Fatalf("POST /login status = %d, body:\n%s", response.StatusCode, body)
	}
	if client.sessionCookie(t) == nil {
		t.Fatal("login did not set a session cookie")
	}
}

func TestRegistrationValidationAndDuplicateEmail(t *testing.T) {
	client := newTestClient(t)
	csrf := client.csrfToken(t, "/register")

	values := registerForm(csrf)
	values.Set("password_confirm", "different password")
	response := client.postForm(t, "/register", values)
	body := readBody(t, response)
	if response.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "Passwords do not match.") {
		t.Fatalf("mismatched password = %d, body:\n%s", response.StatusCode, body)
	}
	if client.sessionCookie(t) != nil {
		t.Fatal("failed registration created a session")
	}

	values = registerForm(csrf)
	response = client.postForm(t, "/register", values)
	readBody(t, response)

	response = client.postForm(t, "/logout", url.Values{"csrf_token": {csrf}})
	readBody(t, response)

	response = client.postForm(t, "/register", registerForm(csrf))
	body = readBody(t, response)
	if response.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "already exists") {
		t.Fatalf("duplicate registration = %d, body:\n%s", response.StatusCode, body)
	}
}

func TestCSRFIsRequired(t *testing.T) {
	client := newTestClient(t)

	response := client.postForm(t, "/login", url.Values{
		"email":    {"ada@example.com"},
		"password": {"correct horse battery staple"},
	})
	body := readBody(t, response)
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("login without CSRF = %d, body:\n%s", response.StatusCode, body)
	}
}

func TestAuthenticatedVisitorsAreRedirectedFromAuthPages(t *testing.T) {
	client := newTestClient(t)
	csrf := client.csrfToken(t, "/register")
	response := client.postForm(t, "/register", registerForm(csrf))
	readBody(t, response)

	response, err := client.noRedirect.Get(client.baseURL + "/login")
	if err != nil {
		t.Fatalf("GET /login: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusSeeOther || response.Header.Get("Location") != "/app" {
		t.Errorf("GET /login = %d %q, want 303 /app", response.StatusCode, response.Header.Get("Location"))
	}
}
