package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCSRFSetsTokenOnSafeRequest(t *testing.T) {
	csrf := NewCSRF(CSRFConfig{})
	var contextToken string
	handler := csrf.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contextToken = CSRFToken(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	cookies := recorder.Result().Cookies()
	require.Len(t, cookies, 1, "cookies")
	assert.Equal(t, DefaultCSRFCookieName, cookies[0].Name, "cookie name")
	assert.False(t, cookies[0].HttpOnly, "CSRF cookie should be readable by scripts")
	assert.Equal(t, cookies[0].Value, contextToken, "context token")
	assert.True(t, validCSRFToken(contextToken), "generated CSRF token is not valid")
}

func TestCSRFRejectsMissingToken(t *testing.T) {
	csrf := NewCSRF(CSRFConfig{})
	handler := csrf.Protect(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		assert.Fail(t, "handler was called for an unverified request")
	}))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(""))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ServeHTTP(recorder, request)

	assert.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestCSRFAcceptsFormToken(t *testing.T) {
	csrf := NewCSRF(CSRFConfig{})
	handler := csrf.Protect(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	const token = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	form := url.Values{"csrf_token": {token}}
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{Name: DefaultCSRFCookieName, Value: token})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)
	assert.Equal(t, http.StatusNoContent, recorder.Code)
}

func TestCSRFAcceptsHeaderToken(t *testing.T) {
	csrf := NewCSRF(CSRFConfig{})
	handler := csrf.Protect(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	const token = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	request := httptest.NewRequest(http.MethodPost, "/api/v1/things", nil)
	request.Header.Set("X-CSRF-Token", token)
	request.AddCookie(&http.Cookie{Name: DefaultCSRFCookieName, Value: token})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)
	assert.Equal(t, http.StatusNoContent, recorder.Code)
}

func TestCSRFReturnsJSONForAPI(t *testing.T) {
	csrf := NewCSRF(CSRFConfig{})
	handler := csrf.Protect(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		assert.Fail(t, "handler was called for an unverified request")
	}))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/things", nil)
	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusForbidden, recorder.Code, "body = %s", recorder.Body.String())
	assert.Equal(t, "application/json; charset=utf-8", recorder.Header().Get("Content-Type"), "Content-Type")

	var body ErrorResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body), "decode error response")
	assert.Equal(t, "csrf_failed", body.Error.Code, "error code")
}

func TestCSRFReplacesInvalidCookie(t *testing.T) {
	csrf := NewCSRF(CSRFConfig{})
	handler := csrf.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.True(t, validCSRFToken(CSRFToken(r.Context())), "context token is invalid")
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: DefaultCSRFCookieName, Value: "invalid"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	cookies := recorder.Result().Cookies()
	require.Len(t, cookies, 1, "cookies = %+v, want a replacement token", cookies)
	assert.True(t, validCSRFToken(cookies[0].Value), "cookies = %+v, want a replacement token", cookies)
}

func TestCSRFMapsOversizedFormsToRequestTooLarge(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		wantJSON bool
	}{
		{name: "html form", path: "/register"},
		{name: "json api", path: "/api/v1/households", wantJSON: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			csrf := NewCSRF(CSRFConfig{})
			called := false
			handler := csrf.Protect(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				called = true
			}))

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, tt.path,
				strings.NewReader(strings.Repeat("a", maxFormBodyBytes+1)))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.ContentLength = -1
			request.Body = http.MaxBytesReader(recorder, request.Body, maxFormBodyBytes)
			handler.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code, "body = %s", recorder.Body.String())
			assert.False(t, called, "handler was reached for an oversized form body")
			if !tt.wantJSON {
				assert.Contains(t, recorder.Body.String(), "request body too large")
				return
			}
			var body ErrorResponse
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body), "decode error response")
			assert.Equal(t, "request_too_large", body.Error.Code, "error code")
		})
	}
}

func TestCSRFKeepsMalformedFormsAtForbidden(t *testing.T) {
	csrf := NewCSRF(CSRFConfig{})
	handler := csrf.Protect(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		assert.Fail(t, "handler was called for a malformed form")
	}))

	const token = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	request := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader("csrf_token=%zz"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{Name: DefaultCSRFCookieName, Value: token})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	assert.Equal(t, http.StatusForbidden, recorder.Code, "body = %s", recorder.Body.String())
}
