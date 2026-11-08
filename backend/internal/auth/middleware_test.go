package auth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
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

	if !ok {
		t.Fatal("FromContext() did not return a session")
	}
	if got.UserID != want.UserID {
		t.Errorf("session user ID = %v, want %v", got.UserID, want.UserID)
	}
}

func TestMiddlewareClearsExpiredSession(t *testing.T) {
	middleware := NewMiddleware(
		stubSessionLookup{err: ErrSessionNotFound},
		CookieConfig{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	handler := middleware.Load(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := FromContext(r.Context()); ok {
			t.Error("FromContext() returned a session for an invalid token")
		}
	}))

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: DefaultSessionCookieName, Value: "raw"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge >= 0 {
		t.Errorf("clear cookie = %+v, want one expired cookie", cookies)
	}
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

	if cookies := recorder.Result().Cookies(); len(cookies) != 0 {
		t.Errorf("transient error cleared session cookie: %+v", cookies)
	}
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
		if _, ok := FromContext(r.Context()); ok {
			t.Error("FromContext() returned a session without a cookie")
		}
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if !called {
		t.Error("next handler was not called")
	}
}
