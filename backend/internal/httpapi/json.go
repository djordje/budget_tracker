package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
)

// maxJSONBodyBytes bounds JSON request bodies.
const maxJSONBodyBytes = 1 << 20

// DecodeError is a safe JSON decoding failure with its response status.
type DecodeError struct {
	Status  int
	Code    string
	Message string
}

// Error implements error without exposing parser details.
func (e *DecodeError) Error() string {
	return e.Code
}

var (
	errJSONMediaType = &DecodeError{
		Status:  http.StatusUnsupportedMediaType,
		Code:    "unsupported_media_type",
		Message: "The request body must use JSON.",
	}
	errJSONMalformed = &DecodeError{
		Status:  http.StatusBadRequest,
		Code:    "invalid_request",
		Message: "The request body must be a single JSON object.",
	}
	errJSONTooLarge = &DecodeError{
		Status:  http.StatusRequestEntityTooLarge,
		Code:    "request_too_large",
		Message: "The request body is too large.",
	}
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

// decodeJSON decodes exactly one JSON object with a bounded body size and a
// strict media type. It rejects null, arrays, scalars, empty bodies, unknown
// fields, and trailing values while allowing trailing whitespace.
func decodeJSON(r *http.Request, destination any) error {
	if err := validateJSONContentType(r.Header.Get("Content-Type")); err != nil {
		return err
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxJSONBodyBytes+1))
	if err != nil {
		return errJSONMalformed
	}
	if len(body) > maxJSONBodyBytes {
		return errJSONTooLarge
	}

	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return errJSONMalformed
	}

	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return errJSONMalformed
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errJSONMalformed
	}
	return nil
}

// WriteDecodeError writes the response for a decode failure. Non-decode errors
// fall back to the malformed-request envelope.
func WriteDecodeError(w http.ResponseWriter, err error) {
	var decodeErr *DecodeError
	if errors.As(err, &decodeErr) {
		WriteError(w, decodeErr.Status, decodeErr.Code, decodeErr.Message)
		return
	}
	WriteError(w, errJSONMalformed.Status, errJSONMalformed.Code, errJSONMalformed.Message)
}

func validateJSONContentType(raw string) error {
	mediaType, params, err := mime.ParseMediaType(raw)
	if err != nil {
		return errJSONMediaType
	}
	jsonType := mediaType == "application/json" ||
		(strings.HasPrefix(mediaType, "application/") && strings.HasSuffix(mediaType, "+json"))
	if !jsonType {
		return errJSONMediaType
	}
	if charset, ok := params["charset"]; ok && !strings.EqualFold(charset, "utf-8") {
		return errJSONMediaType
	}
	return nil
}
