package auth

import (
	"encoding/hex"
	"testing"
)

func TestNewSessionToken(t *testing.T) {
	raw, hash, err := newSessionToken()
	if err != nil {
		t.Fatalf("newSessionToken() error = %v", err)
	}
	if raw == "" {
		t.Fatal("raw token is empty")
	}
	if len(hash) != 64 {
		t.Errorf("hash length = %d, want 64", len(hash))
	}
	if _, err := hex.DecodeString(hash); err != nil {
		t.Errorf("hash is not hexadecimal: %v", err)
	}
	if hash != hashSessionToken(raw) {
		t.Error("returned hash does not match hashSessionToken")
	}
}

func TestSessionTokensAreUnique(t *testing.T) {
	first, _, err := newSessionToken()
	if err != nil {
		t.Fatalf("newSessionToken() error = %v", err)
	}
	second, _, err := newSessionToken()
	if err != nil {
		t.Fatalf("newSessionToken() error = %v", err)
	}
	if first == second {
		t.Error("two generated tokens are identical")
	}
}

func TestHashSessionTokenIsStable(t *testing.T) {
	raw := "known token"
	if hashSessionToken(raw) != hashSessionToken(raw) {
		t.Error("hashSessionToken is not deterministic")
	}
	if hashSessionToken(raw) == hashSessionToken(raw+"x") {
		t.Error("different tokens produced the same hash")
	}
}
