package web

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/google/uuid"

	"budgettracker/internal/auth"
	"budgettracker/internal/finance"
	"budgettracker/internal/households"
	"budgettracker/internal/reporting"
)

type stubSessions struct {
	session  auth.Session
	err      error
	login    func(ctx context.Context, email, password string) (auth.Session, error)
	register func(ctx context.Context, params auth.RegisterParams) (auth.Session, error)
}

func (s stubSessions) Register(ctx context.Context, params auth.RegisterParams) (auth.Session, error) {
	if s.register != nil {
		return s.register(ctx, params)
	}
	return s.session, s.err
}

func (s stubSessions) Login(ctx context.Context, email, password string) (auth.Session, error) {
	if s.login != nil {
		return s.login(ctx, email, password)
	}
	return s.session, s.err
}

func (s stubSessions) DeleteSession(context.Context, string) error {
	return s.err
}

func (s stubSessions) SetActiveHousehold(context.Context, string, uuid.UUID, *uuid.UUID) error {
	return s.err
}

type stubHouseholds struct {
	HouseholdService
	households        []households.Household
	invitations       []households.Invitation
	err               error
	getOptionsErr     error
	createAndActivate func(context.Context, households.CreateParams) (households.Household, error)
}

func (s stubHouseholds) CreateAndActivate(
	ctx context.Context, params households.CreateParams,
) (households.Household, error) {
	if s.createAndActivate != nil {
		return s.createAndActivate(ctx, params)
	}
	return households.Household{}, s.err
}

func (s stubHouseholds) List(context.Context, uuid.UUID) ([]households.Household, error) {
	return s.households, s.err
}

func (s stubHouseholds) Rename(context.Context, uuid.UUID, uuid.UUID, string) (households.Household, error) {
	return households.Household{}, s.err
}

func (s stubHouseholds) Members(context.Context, uuid.UUID, uuid.UUID) ([]households.Member, error) {
	return nil, s.err
}

func (s stubHouseholds) GetOptions(context.Context, uuid.UUID, uuid.UUID) (households.Options, error) {
	if s.getOptionsErr != nil {
		return households.Options{}, s.getOptionsErr
	}
	return households.Options{}, s.err
}

func (s stubHouseholds) GetOptionsView(context.Context, uuid.UUID, uuid.UUID) (households.OptionsView, error) {
	if s.getOptionsErr != nil {
		return households.OptionsView{}, s.getOptionsErr
	}
	return households.OptionsView{}, s.err
}

func (s stubHouseholds) SetUserOptions(context.Context, uuid.UUID, uuid.UUID, households.UpdateUserOptionsParams) (households.OptionsView, error) {
	return households.OptionsView{}, s.err
}

func (s stubHouseholds) RevokeInvitation(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
	return s.err
}

func (s stubHouseholds) UpdateOptions(context.Context, uuid.UUID, uuid.UUID, households.UpdateOptionsParams) (households.Options, error) {
	return households.Options{}, s.err
}

func (s stubHouseholds) CreateInvitation(context.Context, households.CreateInvitationParams) (households.Invitation, string, error) {
	return households.Invitation{}, "", s.err
}

func (s stubHouseholds) ListInvitations(context.Context, uuid.UUID, uuid.UUID) ([]households.Invitation, error) {
	return s.invitations, s.err
}

func (s stubHouseholds) InvitationByToken(context.Context, string) (households.Invitation, error) {
	return households.Invitation{}, s.err
}

func (s stubHouseholds) AcceptInvitation(context.Context, string, uuid.UUID) (households.Invitation, error) {
	return households.Invitation{}, s.err
}

type stubEntries struct{}

func (stubEntries) Currencies(context.Context) ([]finance.Currency, error) { return nil, nil }
func (stubEntries) Create(context.Context, finance.CreateParams) (finance.Entry, error) {
	return finance.Entry{}, nil
}
func (stubEntries) Get(context.Context, uuid.UUID, finance.Kind, int64) (finance.Entry, error) {
	return finance.Entry{}, nil
}
func (stubEntries) Update(context.Context, finance.UpdateParams) (finance.Entry, error) {
	return finance.Entry{}, nil
}
func (stubEntries) Delete(context.Context, uuid.UUID, finance.Kind, int64) error { return nil }
func (stubEntries) List(context.Context, finance.ListParams) (finance.ListResult, error) {
	return finance.ListResult{}, nil
}

type stubRates struct{}

func (stubRates) ListExchangeRates(context.Context, uuid.UUID, uuid.UUID) ([]finance.ExchangeRate, error) {
	return nil, nil
}
func (stubRates) CreateExchangeRate(context.Context, finance.CreateExchangeRateParams) (finance.ExchangeRate, error) {
	return finance.ExchangeRate{}, nil
}
func (stubRates) UpdateExchangeRate(context.Context, finance.UpdateExchangeRateParams) (finance.ExchangeRate, error) {
	return finance.ExchangeRate{}, nil
}
func (stubRates) DeleteExchangeRate(context.Context, uuid.UUID, int64) error { return nil }

type stubReports struct{}

func (stubReports) Summary(context.Context, reporting.SummaryParams) (reporting.Summary, error) {
	return reporting.Summary{}, nil
}

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	handler, err := New(Deps{
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Sessions:   stubSessions{},
		Households: stubHouseholds{},
		Entries:    stubEntries{},
		Rates:      stubRates{},
		Reports:    stubReports{},
	})
	require.NoError(t, err, "New()")
	return handler
}

// serveHouseholdPage serves an authenticated GET from the admin of one
// household that is also the session's active household.
func serveHouseholdPage(t *testing.T, handle func(*Handler) http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	householdID := uuid.New()
	handler, err := New(Deps{
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Sessions: stubSessions{},
		Households: stubHouseholds{
			households: []households.Household{{ID: householdID, Name: "Home", Role: households.RoleAdmin}},
		},
		Entries: stubEntries{},
		Rates:   stubRates{},
		Reports: stubReports{},
	})
	require.NoError(t, err, "New()")

	userID := uuid.New()
	session := auth.Session{
		UserID:            userID,
		User:              auth.User{ID: userID},
		ActiveHouseholdID: &householdID,
	}
	request := httptest.NewRequest(http.MethodGet, householdURL(householdID, ""), nil)
	request.SetPathValue("householdID", householdID.String())
	request.AddCookie(&http.Cookie{Name: auth.DefaultSessionCookieName, Value: "raw"})
	recorder := httptest.NewRecorder()
	auth.NewMiddleware(stubSessionLookup{session: session}, auth.CookieConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil))).
		Load(handler.Scoped("", "", handle(handler))).ServeHTTP(recorder, request)
	return recorder
}

// inputValue returns the value attribute rendered for the named form input.
func inputValue(t *testing.T, body, name string) string {
	t.Helper()
	pattern := regexp.MustCompile(`name="` + regexp.QuoteMeta(name) + `"[^>]*value="([^"]*)"`)
	match := pattern.FindStringSubmatch(body)
	require.Len(t, match, 2, "input %q in body", name)
	return match[1]
}

func TestHome(t *testing.T) {
	handler := newTestHandler(t)
	recorder := httptest.NewRecorder()

	handler.Home(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	require.Equal(t, http.StatusOK, recorder.Code, "body = %s", recorder.Body.String())
	assert.Equal(t, "text/html; charset=utf-8", recorder.Header().Get("Content-Type"), "Content-Type")

	body := recorder.Body.String()
	for _, want := range []string{
		`<html lang="en"`,
		"<title>Budget Tracker</title>",
		"A clear picture of your household",
		`href="/static/css/app.css"`,
		`href="/static/favicon.svg"`,
		"Skip to content",
		`href="/register"`,
		`href="/login"`,
	} {
		assert.Contains(t, body, want)
	}
}

func TestAuthPagesRenderForms(t *testing.T) {
	handler := newTestHandler(t)

	tests := []struct {
		name   string
		path   string
		handle http.HandlerFunc
		want   []string
	}{
		{
			name:   "login",
			path:   "/login",
			handle: handler.LoginPage,
			want:   []string{"Sign in", `action="/login"`, `name="email"`, `name="password"`, `name="csrf_token"`},
		},
		{
			name:   "register",
			path:   "/register",
			handle: handler.RegisterPage,
			want:   []string{"Create your account", `action="/register"`, `name="first_name"`, `name="last_name"`, `name="password_confirm"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			tt.handle(recorder, httptest.NewRequest(http.MethodGet, tt.path, nil))
			require.Equal(t, http.StatusOK, recorder.Code, "body = %s", recorder.Body.String())
			body := recorder.Body.String()
			for _, want := range tt.want {
				assert.Contains(t, body, want)
			}
		})
	}
}

func TestStatic(t *testing.T) {
	handler := newTestHandler(t)
	server := httptest.NewServer(http.StripPrefix("/static/", handler.Static()))
	defer server.Close()

	response, err := http.Get(server.URL + "/static/favicon.svg")
	require.NoError(t, err, "GET static asset")
	defer func() {
		assert.NoError(t, response.Body.Close(), "close response body")
	}()

	assert.Equal(t, http.StatusOK, response.StatusCode)
	assert.Contains(t, response.Header.Get("Content-Type"), "image/svg+xml", "Content-Type")

	body, err := io.ReadAll(response.Body)
	require.NoError(t, err, "read static asset")
	assert.Contains(t, string(body), "<svg", "static asset does not contain SVG markup")
}

func TestStaticDoesNotListDirectories(t *testing.T) {
	handler := newTestHandler(t)
	server := httptest.NewServer(http.StripPrefix("/static/", handler.Static()))
	defer server.Close()

	response, err := http.Get(server.URL + "/static/")
	require.NoError(t, err, "GET static directory")
	defer func() {
		assert.NoError(t, response.Body.Close(), "close response body")
	}()

	assert.Equal(t, http.StatusNotFound, response.StatusCode)
}

func TestStaticRejectsDotfilesAndDirectories(t *testing.T) {
	staticDir := t.TempDir()
	files := map[string]string{
		".env":                  "secret",
		"nested/.secret":        "secret",
		"nested/file.txt":       "visible",
		"css/app.css":           "body {}",
		".well-known/thing.txt": "hidden",
	}
	for name, contents := range files {
		fullPath := filepath.Join(staticDir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o700), "create static fixture directory")
		require.NoError(t, os.WriteFile(fullPath, []byte(contents), 0o600), "write static fixture %s", name)
	}

	handler, err := New(Deps{
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Sessions:   stubSessions{},
		Households: stubHouseholds{},
		Entries:    stubEntries{},
		Rates:      stubRates{},
		Reports:    stubReports{},
		StaticDir:  staticDir,
	})
	require.NoError(t, err, "New()")
	server := httptest.NewServer(http.StripPrefix("/static/", handler.Static()))
	defer server.Close()

	tests := []struct {
		path       string
		wantStatus int
	}{
		{path: "/static/.env", wantStatus: http.StatusNotFound},
		{path: "/static/nested/.secret", wantStatus: http.StatusNotFound},
		{path: "/static/.well-known/thing.txt", wantStatus: http.StatusNotFound},
		{path: "/static/nested", wantStatus: http.StatusNotFound},
		{path: "/static/nested/file.txt", wantStatus: http.StatusOK},
		{path: "/static/css/app.css", wantStatus: http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			response, err := http.Get(server.URL + tt.path)
			require.NoError(t, err, "GET %s", tt.path)
			defer func() {
				assert.NoError(t, response.Body.Close(), "close response body")
			}()
			assert.Equal(t, tt.wantStatus, response.StatusCode)
		})
	}
}

func TestStaticUsesConfiguredDirectory(t *testing.T) {
	staticDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(staticDir, "custom.txt"), []byte("from disk"), 0o600), "write static fixture")

	handler, err := New(Deps{
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Sessions:   stubSessions{},
		Households: stubHouseholds{},
		Entries:    stubEntries{},
		Rates:      stubRates{},
		Reports:    stubReports{},
		StaticDir:  staticDir,
	})
	require.NoError(t, err, "New()")
	server := httptest.NewServer(http.StripPrefix("/static/", handler.Static()))
	defer server.Close()

	response, err := http.Get(server.URL + "/static/custom.txt")
	require.NoError(t, err, "GET configured static asset")
	defer func() {
		assert.NoError(t, response.Body.Close(), "close response body")
	}()

	body, err := io.ReadAll(response.Body)
	require.NoError(t, err, "read configured static asset")
	assert.Equal(t, "from disk", string(body))
}

func TestNewRejectsMissingStaticDirectory(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	_, err := New(Deps{
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Sessions:   stubSessions{},
		Households: stubHouseholds{},
		Entries:    stubEntries{},
		Rates:      stubRates{},
		Reports:    stubReports{},
		StaticDir:  missing,
	})
	assert.Error(t, err, "New() with a missing static directory")
}

func TestNewRequiresSessionService(t *testing.T) {
	_, err := New(Deps{
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Households: stubHouseholds{},
		Entries:    stubEntries{},
		Rates:      stubRates{},
		Reports:    stubReports{},
	})
	assert.Error(t, err, "New() without sessions")
}

func TestNewRequiresHouseholdService(t *testing.T) {
	_, err := New(Deps{
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Sessions: stubSessions{},
		Entries:  stubEntries{},
		Rates:    stubRates{},
		Reports:  stubReports{},
	})
	assert.Error(t, err, "New() without households")
}

func TestNewRequiresEntryService(t *testing.T) {
	_, err := New(Deps{
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Sessions:   stubSessions{},
		Households: stubHouseholds{},
		Rates:      stubRates{},
		Reports:    stubReports{},
	})
	assert.Error(t, err, "New() without entries")
}

func TestNewRequiresRateAndReportServices(t *testing.T) {
	base := Deps{
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Sessions:   stubSessions{},
		Households: stubHouseholds{},
		Entries:    stubEntries{},
		Reports:    stubReports{},
	}
	_, err := New(base)
	assert.Error(t, err, "New() without rates")

	base.Rates = stubRates{}
	base.Reports = nil
	_, err = New(base)
	assert.Error(t, err, "New() without reports")
}

func TestParsePagesRequiresPages(t *testing.T) {
	_, err := parsePages(fstest.MapFS{})
	assert.Error(t, err, "parsePages() with no pages")
}

func TestSafeNextPath(t *testing.T) {
	tests := []struct {
		raw      string
		fallback string
		want     string
	}{
		{raw: "/invitations/token", fallback: "/app", want: "/invitations/token"},
		{raw: "/app?notice=invitation_accepted", fallback: "/app", want: "/app?notice=invitation_accepted"},
		{raw: "/app?page=2", fallback: "/app", want: "/app?page=2"},
		{raw: "?page=2", fallback: "/app", want: "/app"},
		{raw: "", fallback: "/app", want: "/app"},
		{raw: "//evil.example/path", fallback: "/app", want: "/app"},
		{raw: "https://evil.example/path", fallback: "/app", want: "/app"},
		{raw: "javascript:alert(1)", fallback: "/app", want: "/app"},
		{raw: "invitations/token", fallback: "/app", want: "/app"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, safeNextPath(tt.raw, tt.fallback), "safeNextPath(%q)", tt.raw)
	}
}

// TestSafeNextPathKeepsRedirectsOnThisOrigin asserts the safety property for
// inputs a browser could read as protocol-relative or cross-origin instead of
// asserting one accepted string: the result is always a non-empty path on this
// origin. A backslash authority such as /\evil.example stays on this origin
// because the parser escapes the backslash rather than treating it as a host.
func TestSafeNextPathKeepsRedirectsOnThisOrigin(t *testing.T) {
	fallback := "/app"
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "backslash authority", raw: `/\evil.example`, want: "/%5Cevil.example"},
		{name: "encoded slashes", raw: "/%2f%2fevil.example"},
		{name: "protocol relative", raw: "//evil.example"},
		{name: "absolute https URL", raw: "https://evil.example/path"},
		{name: "absolute http URL", raw: "http://evil.example"},
		{name: "javascript scheme", raw: "javascript:alert(1)"},
		{name: "nul byte", raw: "/app\x00page"},
		{name: "newline", raw: "/app\nLocation: https://evil.example"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := safeNextPath(tt.raw, fallback)
			require.NotEmpty(t, got, "safeNextPath(%q)", tt.raw)
			assert.True(t, strings.HasPrefix(got, "/"), "safeNextPath(%q) = %q", tt.raw, got)
			assert.False(t, strings.HasPrefix(got, "//"), "safeNextPath(%q) = %q", tt.raw, got)
			assert.NotContains(t, got, `\`, "safeNextPath(%q) = %q kept a raw backslash", tt.raw, got)
			if tt.want != "" {
				assert.Equal(t, tt.want, got, "safeNextPath(%q)", tt.raw)
			}
			parsed, err := url.Parse(got)
			require.NoError(t, err, "safeNextPath(%q) = %q", tt.raw, got)
			assert.False(t, parsed.IsAbs(), "safeNextPath(%q) = %q", tt.raw, got)
			assert.Empty(t, parsed.Scheme, "safeNextPath(%q) = %q", tt.raw, got)
			assert.NotEmpty(t, parsed.Path, "safeNextPath(%q) = %q", tt.raw, got)
			assert.NotRegexp(t, `[\x00-\x1f\x7f]`, got, "safeNextPath(%q) = %q", tt.raw, got)
		})
	}
}

func TestLoginPagePreservesSafeNext(t *testing.T) {
	handler := newTestHandler(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/login?next=%2Finvitations%2Ftoken", nil)
	handler.LoginPage(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code, "body = %s", recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), `name="next" value="/invitations/token"`, "login page did not preserve safe next path")
}
