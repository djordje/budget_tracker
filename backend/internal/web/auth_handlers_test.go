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

func TestRegisterThrottleKeepsTheServiceUncalled(t *testing.T) {
	var calls int
	handler := newAuthHandler(t, stubSessions{
		register: func(context.Context, auth.RegisterParams) (auth.Session, error) {
			calls++
			return auth.Session{}, &auth.ValidationError{
				Fields: map[string]string{"email": "Enter a valid email address."},
			}
		},
	})
	form := url.Values{
		"first_name":       {"Ada"},
		"last_name":        {"Lovelace"},
		"email":            {"ada@example.com"},
		"password":         {"correct horse battery staple"},
		"password_confirm": {"correct horse battery staple"},
	}

	for i := 0; i < 5; i++ {
		recorder := postAuthForm(handler.Register, "/register", form)
		require.Equal(t, http.StatusUnprocessableEntity, recorder.Code, "attempt %d, body = %s", i+1, recorder.Body.String())
	}

	recorder := postAuthForm(handler.Register, "/register", form)

	require.Equal(t, http.StatusTooManyRequests, recorder.Code, "body = %s", recorder.Body.String())
	assert.Equal(t, "900", recorder.Header().Get("Retry-After"), "Retry-After")
	assert.Contains(t, recorder.Body.String(), "Too many registration attempts")
	assert.Equal(t, 5, calls, "Register() calls")
}

func TestRegisterPasswordMismatchDoesNotSpendThrottleBudget(t *testing.T) {
	var calls int
	handler := newAuthHandler(t, stubSessions{
		register: func(context.Context, auth.RegisterParams) (auth.Session, error) {
			calls++
			return auth.Session{}, &auth.ValidationError{
				Fields: map[string]string{"email": "Enter a valid email address."},
			}
		},
	})
	mismatch := url.Values{
		"first_name":       {"Ada"},
		"last_name":        {"Lovelace"},
		"email":            {"ada@example.com"},
		"password":         {"correct horse battery staple"},
		"password_confirm": {"different"},
	}

	for i := 0; i < 6; i++ {
		recorder := postAuthForm(handler.Register, "/register", mismatch)
		require.Equal(t, http.StatusUnprocessableEntity, recorder.Code, "attempt %d, body = %s", i+1, recorder.Body.String())
	}
	assert.Zero(t, calls, "Register() calls for a password mismatch")

	form := url.Values{
		"first_name":       {"Ada"},
		"last_name":        {"Lovelace"},
		"email":            {"ada@example.com"},
		"password":         {"correct horse battery staple"},
		"password_confirm": {"correct horse battery staple"},
	}
	recorder := postAuthForm(handler.Register, "/register", form)

	require.Equal(t, http.StatusUnprocessableEntity, recorder.Code, "body = %s", recorder.Body.String())
	assert.Equal(t, 1, calls, "Register() calls after the mismatch attempts")
	assert.NotContains(t, recorder.Body.String(), "Too many registration attempts", "mismatch attempts spent the throttle budget")
}
