package auth

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHashPasswordRoundTrip(t *testing.T) {
	password := "correct horse battery staple"
	encoded, err := HashPassword(password)
	require.NoError(t, err, "HashPassword()")
	assert.True(t, strings.HasPrefix(encoded, "$argon2id$v=19$"), "hash prefix = %q", encoded)

	matches, err := VerifyPassword(encoded, password)
	require.NoError(t, err, "VerifyPassword()")
	assert.True(t, matches, "VerifyPassword() = false, want true")

	matches, err = VerifyPassword(encoded, "wrong password")
	require.NoError(t, err, "VerifyPassword()")
	assert.False(t, matches, "VerifyPassword() = true for wrong password")
}

func TestHashPasswordUsesFreshSalt(t *testing.T) {
	first, err := HashPassword("same password")
	require.NoError(t, err, "HashPassword()")
	second, err := HashPassword("same password")
	require.NoError(t, err, "HashPassword()")
	assert.NotEqual(t, first, second, "passwords with the same input produced identical hashes")
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
		_, err := VerifyPassword(encoded, "password")
		assert.Error(t, err, "VerifyPassword(%q)", encoded)
	}
}
