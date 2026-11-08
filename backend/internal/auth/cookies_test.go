package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCookieConfigSetAndRead(t *testing.T) {
	config := CookieConfig{Secure: true, Lifetime: 24 * time.Hour}
	recorder := httptest.NewRecorder()
	expires := time.Now().Add(24 * time.Hour)

	config.Set(recorder, "raw-token", expires)

	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != DefaultSessionCookieName {
		t.Errorf("name = %q", cookie.Name)
	}
	if cookie.Value != "raw-token" {
		t.Errorf("value = %q", cookie.Value)
	}
	if !cookie.HttpOnly {
		t.Error("session cookie is not HttpOnly")
	}
	if !cookie.Secure {
		t.Error("session cookie is not Secure")
	}
	if cookie.Path != "/" {
		t.Errorf("path = %q", cookie.Path)
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v", cookie.SameSite)
	}
	if cookie.MaxAge != 86400 {
		t.Errorf("MaxAge = %d, want 86400", cookie.MaxAge)
	}

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(cookie)
	if got := config.Token(request); got != "raw-token" {
		t.Errorf("Token() = %q", got)
	}
}

func TestCookieConfigClear(t *testing.T) {
	recorder := httptest.NewRecorder()
	CookieConfig{}.Clear(recorder)

	cookie := recorder.Result().Cookies()[0]
	if cookie.MaxAge >= 0 {
		t.Errorf("MaxAge = %d, want negative", cookie.MaxAge)
	}
	if !cookie.Expires.Before(time.Now()) {
		t.Errorf("Expires = %v, want in the past", cookie.Expires)
	}
}

func TestCookieConfigCustomName(t *testing.T) {
	config := CookieConfig{Name: "custom_session"}
	recorder := httptest.NewRecorder()
	config.Set(recorder, "token", time.Now().Add(time.Hour))

	if got := recorder.Result().Cookies()[0].Name; got != "custom_session" {
		t.Errorf("name = %q", got)
	}
}
