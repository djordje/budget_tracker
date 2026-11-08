package httpapi

import (
	"encoding/json"
	"net/http"
)

// WriteJSON writes a JSON response with the standard content type and status.
func WriteJSON(w http.ResponseWriter, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n'))
}

// ErrorResponse is the consistent JSON error envelope used by the API.
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail is a machine-readable error with a human-readable message.
type ErrorDetail struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

// WriteError writes a JSON error envelope.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, ErrorResponse{Error: ErrorDetail{Code: code, Message: message}})
}

// WriteValidationError writes a field-level JSON validation error.
func WriteValidationError(w http.ResponseWriter, fields map[string]string) {
	WriteJSON(w, http.StatusUnprocessableEntity, ErrorResponse{Error: ErrorDetail{
		Code:    "validation_failed",
		Message: "Some fields are invalid.",
		Fields:  fields,
	}})
}
