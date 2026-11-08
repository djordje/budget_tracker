package auth

import (
	"net/http"
	"time"
)

// DefaultSessionCookieName is the cookie that carries the raw session token.
const DefaultSessionCookieName = "bt_session"

// CookieConfig controls the browser session cookie.
type CookieConfig struct {
	Name     string
	Secure   bool
	Lifetime time.Duration
}

func (c CookieConfig) resolved() CookieConfig {
	if c.Name == "" {
		c.Name = DefaultSessionCookieName
	}
	if c.Lifetime <= 0 {
		c.Lifetime = SessionLifetime
	}
	return c
}

// Token returns the raw session token from the request cookie.
func (c CookieConfig) Token(r *http.Request) string {
	c = c.resolved()
	cookie, err := r.Cookie(c.Name)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// Set writes the session cookie.
func (c CookieConfig) Set(w http.ResponseWriter, token string, expires time.Time) {
	c = c.resolved()
	http.SetCookie(w, &http.Cookie{
		Name:     c.Name,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(c.Lifetime.Seconds()),
		HttpOnly: true,
		Secure:   c.Secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// Clear expires the session cookie.
func (c CookieConfig) Clear(w http.ResponseWriter) {
	c = c.resolved()
	http.SetCookie(w, &http.Cookie{
		Name:     c.Name,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(1, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   c.Secure,
		SameSite: http.SameSiteLaxMode,
	})
}
