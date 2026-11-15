package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSecurityHeaders(t *testing.T) {
	handler := SecurityHeaders(SecurityOptions{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/app", nil))

	for name, want := range map[string]string{
		"Content-Security-Policy":    contentSecurityPolicy,
		"X-Content-Type-Options":     "nosniff",
		"X-Frame-Options":            "DENY",
		"Referrer-Policy":            "no-referrer",
		"Permissions-Policy":         "camera=(), microphone=(), geolocation=()",
		"Cross-Origin-Opener-Policy": "same-origin",
		"Cache-Control":              "no-store",
	} {
		assert.Equal(t, want, recorder.Header().Get(name), name)
	}
	assert.Empty(t, recorder.Header().Get("Strict-Transport-Security"), "development HSTS")
}

func TestSecurityHeadersProductionHSTSAndStaticCache(t *testing.T) {
	handler := SecurityHeaders(SecurityOptions{Production: true})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/static/css/app.css", nil))

	assert.NotEmpty(t, recorder.Header().Get("Strict-Transport-Security"), "production HSTS is missing")
	assert.Empty(t, recorder.Header().Get("Cache-Control"), "static Cache-Control")
}
