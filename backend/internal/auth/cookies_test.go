package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"time"
)

func TestCookieConfigSetAndRead(t *testing.T) {
	config := CookieConfig{Secure: true, Lifetime: 24 * time.Hour}
	recorder := httptest.NewRecorder()
	expires := time.Now().Add(24 * time.Hour)

	config.Set(recorder, "raw-token", expires)

	cookies := recorder.Result().Cookies()
	require.Len(t, cookies, 1, "cookies")
	cookie := cookies[0]
	assert.Equal(t, DefaultSessionCookieName, cookie.Name, "name")
	assert.Equal(t, "raw-token", cookie.Value, "value")
	assert.True(t, cookie.HttpOnly, "session cookie is not HttpOnly")
	assert.True(t, cookie.Secure, "session cookie is not Secure")
	assert.Equal(t, "/", cookie.Path, "path")
	assert.Equal(t, http.SameSiteLaxMode, cookie.SameSite, "SameSite")
	assert.Equal(t, 86400, cookie.MaxAge, "MaxAge")

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(cookie)
	assert.Equal(t, "raw-token", config.Token(request), "Token()")
}

func TestCookieConfigClear(t *testing.T) {
	recorder := httptest.NewRecorder()
	CookieConfig{}.Clear(recorder)

	cookie := recorder.Result().Cookies()[0]
	assert.Negative(t, cookie.MaxAge, "MaxAge")
	assert.True(t, cookie.Expires.Before(time.Now()), "Expires = %v, want in the past", cookie.Expires)
}

func TestCookieConfigCustomName(t *testing.T) {
	config := CookieConfig{Name: "custom_session"}
	recorder := httptest.NewRecorder()
	config.Set(recorder, "token", time.Now().Add(time.Hour))

	assert.Equal(t, "custom_session", recorder.Result().Cookies()[0].Name, "name")
}
