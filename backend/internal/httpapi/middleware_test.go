package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestID(t *testing.T) {
	var contextID string
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contextID = RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	headerID := recorder.Header().Get("X-Request-ID")
	require.NotEmpty(t, headerID, "X-Request-ID header")
	assert.Equal(t, headerID, contextID, "context request ID")
}

func TestRequestLogger(t *testing.T) {
	var output strings.Builder
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	handler := RequestID(RequestLogger(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("created"))
	})))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/things", nil))

	var entry map[string]any
	require.NoError(t, json.Unmarshal([]byte(output.String()), &entry), "log output: %s", output.String())
	assert.InDelta(t, http.StatusCreated, entry["status"], 0, "status")
	assert.Equal(t, http.MethodPost, entry["method"], "method")
	assert.Equal(t, "/things", entry["path"], "path")
	assert.NotEmpty(t, entry["request_id"], "request_id")
}

func TestRecovererAPIRespondsWithJSON(t *testing.T) {
	var output strings.Builder
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	handler := RequestID(Recoverer(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/things", nil))

	require.Equal(t, http.StatusInternalServerError, recorder.Code, "body = %s", recorder.Body.String())
	assert.Equal(t, "application/json; charset=utf-8", recorder.Header().Get("Content-Type"), "Content-Type")

	var body ErrorResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body), "decode error response")
	assert.Equal(t, "internal_error", body.Error.Code, "error code")
	assert.Contains(t, output.String(), "panic recovered", "log output missing panic message")
}

func TestRecovererHTMLRespondsWithPlainText(t *testing.T) {
	var output strings.Builder
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	handler := RequestID(Recoverer(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/app", nil))

	require.Equal(t, http.StatusInternalServerError, recorder.Code, "body = %s", recorder.Body.String())
	assert.True(t, strings.HasPrefix(recorder.Header().Get("Content-Type"), "text/plain"), "Content-Type = %q", recorder.Header().Get("Content-Type"))
	assert.Contains(t, recorder.Body.String(), "internal server error")
}

func TestRecovererAfterCommitAppendsNothing(t *testing.T) {
	var output strings.Builder
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	handler := Recoverer(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("partial"))
		panic("boom")
	}))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/app", nil))

	assert.Equal(t, http.StatusCreated, recorder.Code)
	assert.Equal(t, "partial", recorder.Body.String(), "body")
	assert.Contains(t, output.String(), "panic recovered", "log output missing panic message")
}

func TestRequestLoggerRedactsInvitationToken(t *testing.T) {
	var output strings.Builder
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	handler := RequestID(RequestLogger(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/invitations/secret-token", nil))

	assert.NotContains(t, output.String(), "secret-token", "invitation token was logged")
	assert.Contains(t, output.String(), "/invitations/[redacted]", "redacted path missing")
}

func TestLimitHTMLFormBodyRejectsOversizedForms(t *testing.T) {
	called := false
	handler := LimitHTMLFormBody(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))

	request := httptest.NewRequest(http.MethodPost, "/app/households",
		strings.NewReader(strings.Repeat("a", maxFormBodyBytes+1)))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code, "body = %s", recorder.Body.String())
	assert.False(t, called, "handler was reached for an oversized form body")
}

func TestLimitHTMLFormBodyBoundsUnknownLengthForms(t *testing.T) {
	var read int
	var readErr error
	handler := LimitHTMLFormBody(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		read = len(body)
		readErr = err
	}))

	request := httptest.NewRequest(http.MethodPost, "/app/households",
		strings.NewReader(strings.Repeat("a", 2*maxFormBodyBytes)))
	request.ContentLength = -1
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	assert.Equal(t, maxFormBodyBytes, read, "bytes read from the bounded body")
	var maxBytesError *http.MaxBytesError
	assert.ErrorAs(t, readErr, &maxBytesError, "body read error")
}

func TestLimitHTMLFormBodyLeavesAPIBodiesToTheJSONAdapter(t *testing.T) {
	handler := LimitHTMLFormBody(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var destination struct {
			Name string `json:"name"`
		}
		WriteDecodeError(w, decodeJSON(r, &destination))
	}))

	request := httptest.NewRequest(http.MethodPost, "/api/v1/households",
		strings.NewReader(`{"name":"`+strings.Repeat("a", maxJSONBodyBytes)+`"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code, "body = %s", recorder.Body.String())
	var response ErrorResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response), "decode error response")
	assert.Equal(t, "request_too_large", response.Error.Code, "error code")
}
