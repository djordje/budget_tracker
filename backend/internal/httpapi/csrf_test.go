package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
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
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}
	if cookies[0].Name != DefaultCSRFCookieName {
		t.Errorf("cookie name = %q", cookies[0].Name)
	}
	if cookies[0].HttpOnly {
		t.Error("CSRF cookie should be readable by scripts")
	}
	if contextToken == "" || contextToken != cookies[0].Value {
		t.Errorf("context token = %q, cookie = %q", contextToken, cookies[0].Value)
	}
	if !validCSRFToken(contextToken) {
		t.Error("generated CSRF token is not valid")
	}
}

func TestCSRFRejectsMissingToken(t *testing.T) {
	csrf := NewCSRF(CSRFConfig{})
	handler := csrf.Protect(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("handler was called for an unverified request")
	}))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(""))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
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
	if recorder.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
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
	if recorder.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
}

func TestCSRFReturnsJSONForAPI(t *testing.T) {
	csrf := NewCSRF(CSRFConfig{})
	handler := csrf.Protect(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("handler was called for an unverified request")
	}))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/things", nil)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	var body ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if body.Error.Code != "csrf_failed" {
		t.Errorf("code = %q, want csrf_failed", body.Error.Code)
	}
}

func TestCSRFReplacesInvalidCookie(t *testing.T) {
	csrf := NewCSRF(CSRFConfig{})
	handler := csrf.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !validCSRFToken(CSRFToken(r.Context())) {
			t.Error("context token is invalid")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: DefaultCSRFCookieName, Value: "invalid"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || !validCSRFToken(cookies[0].Value) {
		t.Errorf("cookies = %+v, want a replacement token", cookies)
	}
}

func TestWriteValidationError(t *testing.T) {
	recorder := httptest.NewRecorder()
	WriteValidationError(recorder, map[string]string{"name": "Name is required."})

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusUnprocessableEntity)
	}
	var body ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error.Code != "validation_failed" || body.Error.Fields["name"] == "" {
		t.Errorf("error body = %+v", body.Error)
	}
}
