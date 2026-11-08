package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
	"time"
)

// DefaultCSRFCookieName carries the double-submit CSRF token.
const DefaultCSRFCookieName = "bt_csrf"

var defaultCSRFLifetime = 7 * 24 * time.Hour

type csrfContextKey struct{}

// CSRFConfig controls the double-submit CSRF cookie.
type CSRFConfig struct {
	CookieName string
	Secure     bool
	Lifetime   time.Duration
}

// CSRF protects unsafe requests with a token that must match the cookie.
type CSRF struct {
	cookieName string
	secure     bool
	lifetime   time.Duration
}

// NewCSRF constructs CSRF middleware.
func NewCSRF(config CSRFConfig) *CSRF {
	if config.CookieName == "" {
		config.CookieName = DefaultCSRFCookieName
	}
	if config.Lifetime <= 0 {
		config.Lifetime = defaultCSRFLifetime
	}
	return &CSRF{
		cookieName: config.CookieName,
		secure:     config.Secure,
		lifetime:   config.Lifetime,
	}
}

// Protect validates unsafe requests and exposes the token in the context.
func (c *CSRF) Protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := tokenFromCookie(r, c.cookieName)
		if !validCSRFToken(token) {
			var err error
			token, err = newCSRFToken()
			if err != nil {
				WriteError(w, http.StatusInternalServerError, "internal_error", "The request could not be processed.")
				return
			}
			c.setCookie(w, token)
		}

		if isUnsafeMethod(r.Method) {
			submitted := r.Header.Get("X-CSRF-Token")
			if submitted == "" {
				submitted = r.PostFormValue("csrf_token")
			}
			if !tokensEqual(token, submitted) {
				if wantsJSON(r) {
					WriteError(w, http.StatusForbidden, "csrf_failed", "The request could not be verified. Refresh the page and try again.")
				} else {
					http.Error(w, "the request could not be verified; refresh the page and try again", http.StatusForbidden)
				}
				return
			}
		}

		ctx := context.WithValue(r.Context(), csrfContextKey{}, token)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// CSRFToken returns the double-submit token attached by Protect.
func CSRFToken(ctx context.Context) string {
	token, _ := ctx.Value(csrfContextKey{}).(string)
	return token
}

func newCSRFToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func validCSRFToken(token string) bool {
	if len(token) != 64 {
		return false
	}
	_, err := hex.DecodeString(token)
	return err == nil
}

func tokensEqual(expected, actual string) bool {
	if len(expected) == 0 || len(expected) != len(actual) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) == 1
}

func tokenFromCookie(r *http.Request, name string) string {
	cookie, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func (c *CSRF) setCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     c.cookieName,
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(c.lifetime),
		MaxAge:   int(c.lifetime.Seconds()),
		HttpOnly: false,
		Secure:   c.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func isUnsafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return false
	default:
		return true
	}
}

func wantsJSON(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, "/api/") ||
		strings.Contains(r.Header.Get("Accept"), "application/json")
}
