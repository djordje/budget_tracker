package auth

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeEmail(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "trims and lowercases", input: "  User@Example.COM ", want: "user@example.com"},
		{name: "plain address", input: "a@example.com", want: "a@example.com"},
		{name: "empty", input: "", wantErr: true},
		{name: "missing at", input: "not-an-email", wantErr: true},
		{name: "display name", input: "Person <person@example.com>", wantErr: true},
		{name: "trailing junk", input: "person@example.com extra", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeEmail(tt.input)
			if tt.wantErr {
				require.Error(t, err, "NormalizeEmail(%q) = %q", tt.input, got)
				return
			}
			require.NoError(t, err, "NormalizeEmail()")
			assert.Equal(t, tt.want, got, "NormalizeEmail()")
		})
	}
}

func TestValidateRegistration(t *testing.T) {
	valid := RegisterParams{
		FirstName: "  Ada ",
		LastName:  " Lovelace ",
		Email:     " ADA@Example.com ",
		Password:  "correct horse battery staple",
	}
	email, err := validateRegistration(valid)
	require.Nil(t, err, "validateRegistration()")
	assert.Equal(t, "ada@example.com", email, "email")

	tests := []struct {
		name  string
		param RegisterParams
		field string
	}{
		{name: "missing first name", param: RegisterParams{LastName: "L", Email: "a@example.com", Password: "correct horse battery"}, field: "first_name"},
		{name: "missing last name", param: RegisterParams{FirstName: "A", Email: "a@example.com", Password: "correct horse battery"}, field: "last_name"},
		{name: "invalid email", param: RegisterParams{FirstName: "A", LastName: "L", Email: "invalid", Password: "correct horse battery"}, field: "email"},
		{name: "short password", param: RegisterParams{FirstName: "A", LastName: "L", Email: "a@example.com", Password: "short"}, field: "password"},
		{name: "long password", param: RegisterParams{FirstName: "A", LastName: "L", Email: "a@example.com", Password: strings.Repeat("x", 129)}, field: "password"},
		{name: "invalid UTF-8 first name", param: RegisterParams{FirstName: string([]byte{'A', 0xff}), LastName: "L", Email: "a@example.com", Password: "correct horse battery"}, field: "first_name"},
		{name: "NUL last name", param: RegisterParams{FirstName: "A", LastName: "L\x00ovelace", Email: "a@example.com", Password: "correct horse battery"}, field: "last_name"},
		{name: "control character first name", param: RegisterParams{FirstName: "A\nB", LastName: "L", Email: "a@example.com", Password: "correct horse battery"}, field: "first_name"},
		{name: "too long first name", param: RegisterParams{FirstName: strings.Repeat("a", 81), LastName: "L", Email: "a@example.com", Password: "correct horse battery"}, field: "first_name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// validateRegistration returns *ValidationError, so a nil result is a
			// typed nil: require.Error would see a non-nil error interface.
			_, err := validateRegistration(tt.param)
			require.NotNil(t, err, "validateRegistration()")
			assert.Contains(t, err.Fields, tt.field, "fields = %v", err.Fields)
		})
	}
}

func TestValidateRegistrationAcceptsUnicodeNames(t *testing.T) {
	_, err := validateRegistration(RegisterParams{
		FirstName: "  Ana María ",
		LastName:  "Šimić",
		Email:     "ana@example.com",
		Password:  "correct horse battery",
	})
	assert.Nil(t, err, "validateRegistration()")
}
