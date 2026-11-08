package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
		if got := recorder.Header().Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if got := recorder.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("development HSTS = %q, want empty", got)
	}
}

func TestSecurityHeadersProductionHSTSAndStaticCache(t *testing.T) {
	handler := SecurityHeaders(SecurityOptions{Production: true})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/static/css/app.css", nil))

	if got := recorder.Header().Get("Strict-Transport-Security"); got == "" {
		t.Error("production HSTS is missing")
	}
	if got := recorder.Header().Get("Cache-Control"); got != "" {
		t.Errorf("static Cache-Control = %q, want empty", got)
	}
}
