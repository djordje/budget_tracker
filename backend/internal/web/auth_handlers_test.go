package web

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"budgettracker/internal/auth"
)

func newAuthHandler(t *testing.T, sessions SessionService) *Handler {
	t.Helper()
	handler, err := New(Deps{
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Sessions:   sessions,
		Households: stubHouseholds{},
		Entries:    stubEntries{},
		Rates:      stubRates{},
		Reports:    stubReports{},
	})
	require.NoError(t, err, "New()")
	return handler
}

func postAuthForm(handle http.HandlerFunc, path string, values url.Values) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	handle(recorder, request)
	return recorder
}

func TestLoginRejectsOversizedPasswordWithoutCallingService(t *testing.T) {
	var calls int
	handler := newAuthHandler(t, stubSessions{
		login: func(context.Context, string, string) (auth.Session, error) {
			calls++
			return auth.Session{}, auth.ErrInvalidCredentials
		},
	})

	recorder := postAuthForm(handler.Login, "/login", url.Values{
		"email":    {"ada@example.com"},
		"password": {strings.Repeat("x", auth.PasswordMaxLength+1)},
	})

	require.Equal(t, http.StatusUnprocessableEntity, recorder.Code, "body = %s", recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), "Email or password is incorrect.")
	assert.Zero(t, calls, "Login() calls")
}
