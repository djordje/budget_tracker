package auth

import (
	"strings"
	"testing"
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
				if err == nil {
					t.Fatalf("NormalizeEmail() = %q, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeEmail() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("NormalizeEmail() = %q, want %q", got, tt.want)
			}
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
	if err != nil {
		t.Fatalf("validateRegistration() error = %v", err)
	}
	if email != "ada@example.com" {
		t.Errorf("email = %q, want ada@example.com", email)
	}

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
			_, err := validateRegistration(tt.param)
			if err == nil {
				t.Fatal("validateRegistration() error = nil, want error")
			}
			if _, ok := err.Fields[tt.field]; !ok {
				t.Errorf("fields = %v, want %q", err.Fields, tt.field)
			}
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
	if err != nil {
		t.Fatalf("validateRegistration() error = %v", err)
	}
}
