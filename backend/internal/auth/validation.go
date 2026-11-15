package auth

import (
	"errors"
	"net/mail"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ValidationError describes invalid registration input by field.
type ValidationError struct {
	Fields map[string]string
}

// Error implements error.
func (e *ValidationError) Error() string {
	return "validation failed"
}

func newValidationError() *ValidationError {
	return &ValidationError{Fields: make(map[string]string)}
}

func (e *ValidationError) add(field, message string) {
	if _, exists := e.Fields[field]; !exists {
		e.Fields[field] = message
	}
}

// NormalizeEmail trims and lowercases an email address and verifies its basic
// address syntax.
func NormalizeEmail(raw string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if len(normalized) < 3 || len(normalized) > 254 {
		return "", errInvalidEmail
	}

	address, err := mail.ParseAddress(normalized)
	if err != nil || address.Address != normalized {
		return "", errInvalidEmail
	}

	return normalized, nil
}

func validateRegistration(params RegisterParams) (string, *ValidationError) {
	validation := newValidationError()

	validatePersonName(params.FirstName, "first_name", "First name", validation)
	validatePersonName(params.LastName, "last_name", "Last name", validation)

	email, err := NormalizeEmail(params.Email)
	if err != nil {
		validation.add("email", "Enter a valid email address.")
	}

	switch {
	case len(params.Password) < PasswordMinLength:
		validation.add("password", "Password must be at least 12 characters.")
	case len(params.Password) > PasswordMaxLength:
		validation.add("password", "Password must be 128 characters or fewer.")
	}

	if len(validation.Fields) > 0 {
		return "", validation
	}
	return email, nil
}

func validatePersonName(raw, field, label string, validation *ValidationError) {
	name := strings.TrimSpace(raw)
	switch {
	case name == "":
		validation.add(field, label+" is required.")
	case !utf8.ValidString(name):
		validation.add(field, label+" contains invalid characters.")
	case utf8.RuneCountInString(name) > 80:
		validation.add(field, label+" must be 80 characters or fewer.")
	case strings.IndexFunc(name, unicode.IsControl) >= 0:
		validation.add(field, label+" contains invalid characters.")
	}
}

var errInvalidEmail = errors.New("invalid email address")
