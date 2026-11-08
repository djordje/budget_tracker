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

func TestDecodeJSONPreservesSingleObjectContract(t *testing.T) {
	largeBody := `{"name":"` + strings.Repeat("a", 1<<20) + `"}`
	tests := []struct {
		name        string
		contentType string
		body        string
		wantErr     bool
	}{
		{name: "valid object", contentType: "application/json", body: `{"name":"Home"}`},
		{name: "valid object with charset", contentType: "application/json; charset=utf-8", body: `{"name":"Home"}`},
		{name: "trailing whitespace", contentType: "application/json", body: "{\"name\":\"Home\"}\n\t"},
		{name: "missing content type", body: `{"name":"Home"}`, wantErr: true},
		{name: "wrong content type", contentType: "text/plain", body: `{"name":"Home"}`, wantErr: true},
		{name: "unknown field", contentType: "application/json", body: `{"name":"Home","extra":true}`, wantErr: true},
		{name: "trailing value", contentType: "application/json", body: `{"name":"Home"}{"name":"Other"}`, wantErr: true},
		{name: "empty body", contentType: "application/json", body: "", wantErr: true},
		{name: "over body limit", contentType: "application/json", body: largeBody, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/households", strings.NewReader(tt.body))
			if tt.contentType != "" {
				request.Header.Set("Content-Type", tt.contentType)
			}
			var destination struct {
				Name string `json:"name"`
			}
			err := decodeJSON(recorder, request, &destination)
			if tt.wantErr && err == nil {
				t.Fatal("decodeJSON() error = nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("decodeJSON() error = %v", err)
			}
		})
	}
}
