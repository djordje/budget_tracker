package auth

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSessionToken(t *testing.T) {
	raw, hash, err := newSessionToken()
	require.NoError(t, err, "newSessionToken()")
	assert.NotEmpty(t, raw, "raw token is empty")
	assert.Len(t, hash, 64, "hash length")
	_, err = hex.DecodeString(hash)
	assert.NoError(t, err, "hash is not hexadecimal")
	assert.Equal(t, hashSessionToken(raw), hash, "returned hash does not match hashSessionToken")
}

func TestSessionTokensAreUnique(t *testing.T) {
	first, _, err := newSessionToken()
	require.NoError(t, err, "newSessionToken()")
	second, _, err := newSessionToken()
	require.NoError(t, err, "newSessionToken()")
	assert.NotEqual(t, first, second, "two generated tokens are identical")
}

func TestHashSessionTokenIsStable(t *testing.T) {
	raw := "known token"
	first := hashSessionToken(raw)
	assert.Equal(t, first, hashSessionToken(raw), "hashSessionToken is not deterministic")
	assert.NotEqual(t, first, hashSessionToken(raw+"x"), "different tokens produced the same hash")
}
