package web

import (
	"bytes"
	"errors"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bufferedLogger returns a logger writing structured text lines into buffer.
func bufferedLogger(buffer *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buffer, nil))
}

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

func TestLogPathRedactsInvitationTokens(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: "/invitations/secret-token", want: "/invitations/[redacted]"},
		{path: "/invitations/", want: "/invitations/[redacted]"},
		{path: "/app/invitations/secret-token", want: "/app/invitations/secret-token"},
		{path: "/app/entries", want: "/app/entries"},
	}
	for _, tt := range tests {
		request := httptest.NewRequest(http.MethodGet, tt.path, nil)
		assert.Equal(t, tt.want, logPath(request), "logPath(%q)", tt.path)
	}
}

func TestInvitationErrorsLogRedactedPath(t *testing.T) {
	const token = "super-secret-invitation-token"
	var logOutput bytes.Buffer
	handler, err := New(Deps{
		Logger:     bufferedLogger(&logOutput),
		Sessions:   stubSessions{},
		Households: stubHouseholds{err: errors.New("database unavailable")},
		Entries:    stubEntries{},
		Rates:      stubRates{},
		Reports:    stubReports{},
	})
	require.NoError(t, err, "New()")

	request := httptest.NewRequest(http.MethodGet, "/invitations/"+token, nil)
	request.SetPathValue("token", token)
	recorder := httptest.NewRecorder()

	handler.InvitationPage(recorder, request)

	require.Equal(t, http.StatusInternalServerError, recorder.Code, "body = %s", recorder.Body.String())
	assert.Contains(t, logOutput.String(), "/invitations/[redacted]", "log output")
	assert.NotContains(t, logOutput.String(), token, "log output leaked an invitation token")
}

func TestRenderFailuresLogRedactedPath(t *testing.T) {
	const token = "super-secret-invitation-token"
	var logOutput bytes.Buffer
	page := template.Must(template.New("layout").Parse(`partial{{index . 5}}`))
	handler := &Handler{
		pages:  map[string]*template.Template{"broken": page},
		logger: bufferedLogger(&logOutput),
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/invitations/"+token, nil)

	handler.render(recorder, request, "broken", http.StatusOK, []string{"only"})

	require.Equal(t, http.StatusInternalServerError, recorder.Code, "body = %s", recorder.Body.String())
	assert.Contains(t, logOutput.String(), "/invitations/[redacted]", "log output")
	assert.NotContains(t, logOutput.String(), token, "log output leaked an invitation token")
}

func TestNoticeFromQueryExplainsUnselectedHousehold(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/app?notice=invitation_accepted_household_not_selected", nil)

	assert.Equal(t,
		"You joined the household. Select it from the household menu to make it active.",
		noticeFromQuery(request),
		"notice copy",
	)
}
