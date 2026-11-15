package web

import (
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	require.Equal(t, http.StatusInternalServerError, recorder.Code, "body = %q", recorder.Body.String())
	assert.NotContains(t, recorder.Body.String(), "partial", "failed render wrote a partial body")
	assert.Contains(t, recorder.Body.String(), "internal server error")
}

func TestBasePageDataComputesCurrentYear(t *testing.T) {
	handler := &Handler{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	data := handler.basePageData(httptest.NewRequest(http.MethodGet, "/", nil), "Home")

	assert.Equal(t, time.Now().Year(), data.Year, "Year")
}
