package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

const sessionTokenBytes = 32

func newSessionToken() (raw, hash string, err error) {
	token := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(token); err != nil {
		return "", "", fmt.Errorf("generate session token: %w", err)
	}
	raw = base64.RawURLEncoding.EncodeToString(token)
	return raw, hashSessionToken(raw), nil
}

func hashSessionToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
