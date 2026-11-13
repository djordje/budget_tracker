package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteValidationError(t *testing.T) {
	recorder := httptest.NewRecorder()
	WriteValidationError(recorder, map[string]string{"name": "Name is required."})

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusUnprocessableEntity)
	}
	var body ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error.Code != "validation_failed" || body.Error.Fields["name"] == "" {
		t.Errorf("error body = %+v", body.Error)
	}
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
			if status != tt.wantStatus || code != tt.wantCode {
				t.Errorf("status/code = %d/%q, want %d/%q", status, code, tt.wantStatus, tt.wantCode)
			}
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
			if status != tt.wantStatus || code != tt.wantCode {
				t.Errorf("status/code = %d/%q, want %d/%q", status, code, tt.wantStatus, tt.wantCode)
			}
		})
	}
}

func TestDecodeJSONBodyLimit(t *testing.T) {
	exact := jsonObjectOfSize(maxJSONBodyBytes)
	over := jsonObjectOfSize(maxJSONBodyBytes + 1)

	status, code, _ := decodeAndRespond(t, "application/json", exact)
	if status != 0 || code != "" {
		t.Errorf("exact-limit body status/code = %d/%q, want success", status, code)
	}

	status, code, _ = decodeAndRespond(t, "application/json", over)
	if status != http.StatusRequestEntityTooLarge || code != "request_too_large" {
		t.Errorf("over-limit body status/code = %d/%q, want 413/request_too_large", status, code)
	}
}

func TestDecodeJSONErrorResponsesAreSafeJSON(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/households", strings.NewReader(`{"name":}`))
	request.Header.Set("Content-Type", "application/json")
	var destination struct {
		Name string `json:"name"`
	}

	err := decodeJSON(recorder, request, &destination)
	if err == nil {
		t.Fatal("decodeJSON() error = nil, want error")
	}
	WriteDecodeError(recorder, err)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	var body ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if body.Error.Code != "invalid_request" {
		t.Errorf("code = %q", body.Error.Code)
	}
	if strings.Contains(strings.ToLower(body.Error.Message), "invalid character") {
		t.Errorf("error message leaked parser details: %q", body.Error.Message)
	}
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
	if unmarshalErr := json.Unmarshal(recorder.Body.Bytes(), &response); unmarshalErr != nil {
		t.Fatalf("decode error response: %v", unmarshalErr)
	}
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
