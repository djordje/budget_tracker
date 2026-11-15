package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteValidationError(t *testing.T) {
	recorder := httptest.NewRecorder()
	WriteValidationError(recorder, map[string]string{"name": "Name is required."})

	assert.Equal(t, http.StatusUnprocessableEntity, recorder.Code)

	var body ErrorResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body), "decode response")
	assert.Equal(t, "validation_failed", body.Error.Code, "error body = %+v", body.Error)
	assert.NotEmpty(t, body.Error.Fields["name"], "error body = %+v", body.Error)
}

func TestDecodeJSONMediaTypes(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		wantStatus  int
		wantCode    string
	}{
		{name: "json", contentType: "application/json"},
		{name: "json with charset", contentType: "application/json; charset=utf-8"},
		{name: "json with uppercase charset", contentType: "application/json; charset=UTF-8"},
		{name: "structured suffix", contentType: "application/vnd.api+json"},
		{name: "missing", contentType: "", wantStatus: http.StatusUnsupportedMediaType, wantCode: "unsupported_media_type"},
		{name: "text json", contentType: "text/json", wantStatus: http.StatusUnsupportedMediaType, wantCode: "unsupported_media_type"},
		{name: "json lookalike", contentType: "application/jsonp", wantStatus: http.StatusUnsupportedMediaType, wantCode: "unsupported_media_type"},
		{name: "form encoded", contentType: "application/x-www-form-urlencoded", wantStatus: http.StatusUnsupportedMediaType, wantCode: "unsupported_media_type"},
		{name: "unsupported charset", contentType: "application/json; charset=latin1", wantStatus: http.StatusUnsupportedMediaType, wantCode: "unsupported_media_type"},
		{name: "malformed parameter", contentType: "application/json; charset", wantStatus: http.StatusUnsupportedMediaType, wantCode: "unsupported_media_type"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, code, _ := decodeAndRespond(t, tt.contentType, `{"name":"Home"}`)
			assert.Equal(t, tt.wantStatus, status, "status")
			assert.Equal(t, tt.wantCode, code, "error code")
		})
	}
}

func TestDecodeJSONBodyShapes(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{name: "object", body: `{"name":"Home"}`},
		{name: "object with whitespace", body: " \n\t{\"name\":\"Home\"}\r\n"},
		{name: "null", body: `null`, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "array", body: `[{"name":"Home"}]`, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "string", body: `"Home"`, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "number", body: `42`, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "boolean", body: `true`, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "empty", body: ``, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "whitespace only", body: " \n\t", wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "malformed", body: `{"name":`, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "unknown field", body: `{"name":"Home","extra":true}`, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "trailing value", body: `{"name":"Home"}{"name":"Other"}`, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, code, _ := decodeAndRespond(t, "application/json", tt.body)
			assert.Equal(t, tt.wantStatus, status, "status")
			assert.Equal(t, tt.wantCode, code, "error code")
		})
	}
}

func TestDecodeJSONBodyLimit(t *testing.T) {
	exact := jsonObjectOfSize(maxJSONBodyBytes)
	over := jsonObjectOfSize(maxJSONBodyBytes + 1)

	status, code, _ := decodeAndRespond(t, "application/json", exact)
	assert.Zero(t, status, "exact-limit body status")
	assert.Empty(t, code, "exact-limit body error code")

	status, code, _ = decodeAndRespond(t, "application/json", over)
	assert.Equal(t, http.StatusRequestEntityTooLarge, status, "over-limit body status")
	assert.Equal(t, "request_too_large", code, "over-limit body error code")
}

func TestDecodeJSONErrorResponsesAreSafeJSON(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/households", strings.NewReader(`{"name":}`))
	request.Header.Set("Content-Type", "application/json")
	var destination struct {
		Name string `json:"name"`
	}

	err := decodeJSON(recorder, request, &destination)
	require.Error(t, err, "decodeJSON()")
	WriteDecodeError(recorder, err)

	require.Equal(t, http.StatusBadRequest, recorder.Code, "body = %s", recorder.Body.String())
	assert.Equal(t, "application/json; charset=utf-8", recorder.Header().Get("Content-Type"), "Content-Type")

	var body ErrorResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body), "decode error response")
	assert.Equal(t, "invalid_request", body.Error.Code, "error code")
	assert.NotContains(t, strings.ToLower(body.Error.Message), "invalid character", "error message leaked parser details: %q", body.Error.Message)
}

func decodeAndRespond(t *testing.T, contentType, body string) (int, string, string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/households", strings.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	var destination struct {
		Name string `json:"name"`
	}
	err := decodeJSON(recorder, request, &destination)
	if err == nil {
		return 0, "", ""
	}
	WriteDecodeError(recorder, err)

	var response ErrorResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response), "decode error response")
	return recorder.Code, response.Error.Code, response.Error.Message
}

func jsonObjectOfSize(size int) string {
	const prefix = `{"name":"`
	const suffix = `"}`
	if size <= len(prefix)+len(suffix) {
		return prefix + suffix
	}
	return prefix + strings.Repeat("a", size-len(prefix)-len(suffix)) + suffix
}
