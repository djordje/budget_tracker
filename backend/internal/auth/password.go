package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonMemory      uint32 = 64 * 1024 // KiB
	argonIterations  uint32 = 3
	argonParallelism uint8  = 2
	argonSaltLength         = 16
	argonKeyLength          = 32
)

var errInvalidPasswordHash = errors.New("invalid password hash")

// HashPassword hashes a password with Argon2id using a fresh random salt.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}

	key := argon2.IDKey([]byte(password), salt, argonIterations, argonMemory, argonParallelism, argonKeyLength)
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argonMemory,
		argonIterations,
		argonParallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword reports whether password matches an encoded Argon2id hash.
func VerifyPassword(encoded, password string) (bool, error) {
	params, salt, expected, err := parsePasswordHash(encoded)
	if err != nil {
		return false, err
	}

	actual := argon2.IDKey([]byte(password), salt, params.iterations, params.memory, params.parallelism, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

type passwordHashParams struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
}

func parsePasswordHash(encoded string) (passwordHashParams, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return passwordHashParams{}, nil, nil, errInvalidPasswordHash
	}

	version, err := strconv.Atoi(strings.TrimPrefix(parts[2], "v="))
	if err != nil || version != argon2.Version {
		return passwordHashParams{}, nil, nil, errInvalidPasswordHash
	}

	params, err := parseParams(parts[3])
	if err != nil {
		return passwordHashParams{}, nil, nil, err
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return passwordHashParams{}, nil, nil, errInvalidPasswordHash
	}

	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) < 16 || len(key) > 64 {
		return passwordHashParams{}, nil, nil, errInvalidPasswordHash
	}

	return params, salt, key, nil
}

func parseParams(encoded string) (passwordHashParams, error) {
	values := strings.Split(encoded, ",")
	if len(values) != 3 {
		return passwordHashParams{}, errInvalidPasswordHash
	}

	memory, err := parseUintParam(values[0], "m=", 32)
	if err != nil {
		return passwordHashParams{}, err
	}
	iterations, err := parseUintParam(values[1], "t=", 32)
	if err != nil {
		return passwordHashParams{}, err
	}
	parallelism, err := parseUintParam(values[2], "p=", 8)
	if err != nil {
		return passwordHashParams{}, err
	}

	// Bound stored parameters so a malformed hash cannot trigger excessive
	// allocations or work.
	if memory < 8*1024 || memory > 256*1024 ||
		iterations < 1 || iterations > 10 ||
		parallelism < 1 || parallelism > 16 {
		return passwordHashParams{}, errInvalidPasswordHash
	}

	return passwordHashParams{
		memory:      uint32(memory),
		iterations:  uint32(iterations),
		parallelism: uint8(parallelism),
	}, nil
}

func parseUintParam(value, prefix string, bits int) (uint64, error) {
	raw, ok := strings.CutPrefix(value, prefix)
	if !ok {
		return 0, errInvalidPasswordHash
	}
	parsed, err := strconv.ParseUint(raw, 10, bits)
	if err != nil {
		return 0, errInvalidPasswordHash
	}
	return parsed, nil
}
