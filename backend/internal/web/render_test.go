package web

import (
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRenderTemplateBuffersExecutionFailures(t *testing.T) {
	page := template.Must(template.New("layout").Parse(`partial{{index . 5}}`))
	handler := &Handler{
		pages:  map[string]*template.Template{"broken": page},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/app/entries", nil)

	handler.render(recorder, request, "broken", http.StatusOK, []string{"only"})

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if strings.Contains(recorder.Body.String(), "partial") {
		t.Errorf("failed render wrote a partial body: %q", recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "internal server error") {
		t.Errorf("body = %q", recorder.Body.String())
	}
}

func TestBasePageDataComputesCurrentYear(t *testing.T) {
	handler := &Handler{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	data := handler.basePageData(httptest.NewRequest(http.MethodGet, "/", nil), "Home")

	if data.Year != time.Now().Year() {
		t.Errorf("Year = %d, want %d", data.Year, time.Now().Year())
	}
}
