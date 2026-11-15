package auth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"time"

	"github.com/google/uuid"
)

type stubSessionLookup struct {
	session Session
	err     error
}

func (s stubSessionLookup) SessionByToken(context.Context, string) (Session, error) {
	return s.session, s.err
}

func TestMiddlewareLoadsSession(t *testing.T) {
	want := Session{UserID: uuid.New(), ExpiresAt: time.Now().Add(time.Hour)}
	middleware := NewMiddleware(
		stubSessionLookup{session: want},
		CookieConfig{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	var got Session
	var ok bool
	handler := middleware.Load(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok = FromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: DefaultSessionCookieName, Value: "raw"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	require.True(t, ok, "FromContext() did not return a session")
	assert.Equal(t, want.UserID, got.UserID, "session user ID")
}

func TestMiddlewareClearsExpiredSession(t *testing.T) {
	middleware := NewMiddleware(
		stubSessionLookup{err: ErrSessionNotFound},
		CookieConfig{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	handler := middleware.Load(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, ok := FromContext(r.Context())
		assert.False(t, ok, "FromContext() returned a session for an invalid token")
	}))

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: DefaultSessionCookieName, Value: "raw"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	cookies := recorder.Result().Cookies()
	require.Len(t, cookies, 1, "clear cookie = %+v, want one expired cookie", cookies)
	assert.Negative(t, cookies[0].MaxAge, "clear cookie = %+v, want one expired cookie", cookies)
}

func TestMiddlewareKeepsCookieOnTransientError(t *testing.T) {
	middleware := NewMiddleware(
		stubSessionLookup{err: errors.New("database unavailable")},
		CookieConfig{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	handler := middleware.Load(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: DefaultSessionCookieName, Value: "raw"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	assert.Empty(t, recorder.Result().Cookies(), "transient error cleared session cookie")
}

func TestMiddlewareWithoutCookie(t *testing.T) {
	called := false
	middleware := NewMiddleware(
		stubSessionLookup{},
		CookieConfig{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	handler := middleware.Load(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, ok := FromContext(r.Context())
		assert.False(t, ok, "FromContext() returned a session without a cookie")
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	assert.True(t, called, "next handler was not called")
}
