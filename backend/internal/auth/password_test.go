package auth

import (
	"strings"
	"testing"
)

func TestHashPasswordRoundTrip(t *testing.T) {
	password := "correct horse battery staple"
	encoded, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$") {
		t.Errorf("hash has unexpected prefix: %q", encoded)
	}

	matches, err := VerifyPassword(encoded, password)
	if err != nil {
		t.Fatalf("VerifyPassword() error = %v", err)
	}
	if !matches {
		t.Error("VerifyPassword() = false, want true")
	}

	matches, err = VerifyPassword(encoded, "wrong password")
	if err != nil {
		t.Fatalf("VerifyPassword() error = %v", err)
	}
	if matches {
		t.Error("VerifyPassword() = true for wrong password")
	}
}

func TestHashPasswordUsesFreshSalt(t *testing.T) {
	first, err := HashPassword("same password")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	second, err := HashPassword("same password")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if first == second {
		t.Error("passwords with the same input produced identical hashes")
	}
}

func TestVerifyPasswordRejectsMalformedHash(t *testing.T) {
	tests := []string{
		"",
		"not-a-hash",
		"$argon2id$v=19$m=65536,t=3$salt$key",
		"$argon2id$v=19$m=65536,t=3,p=2$bad-salt$bad-key",
		"$argon2id$v=19$m=1,t=3,p=2$c2FsdHNhbHRzYWx0$c2FsdHNhbHRzYWx0c2FsdHNhbHRzYWx0",
	}

	for _, encoded := range tests {
		if _, err := VerifyPassword(encoded, "password"); err == nil {
			t.Errorf("VerifyPassword(%q) error = nil, want error", encoded)
		}
	}
}
