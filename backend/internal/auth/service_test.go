package auth

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"time"
)

func TestLoginVerifiesPasswordForMissingAndKnownEmail(t *testing.T) {
	tests := []struct {
		name        string
		lookup      credentialLookup
		wantEncoded string
	}{
		{
			name: "unknown email",
			lookup: func(context.Context, string) (User, string, error) {
				return User{}, "", pgx.ErrNoRows
			},
			wantEncoded: dummyPasswordHash,
		},
		{
			name: "known email with a wrong password",
			lookup: func(context.Context, string) (User, string, error) {
				return User{Email: "ada@example.com"}, "stored-hash", nil
			},
			wantEncoded: "stored-hash",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewService(nil)
			service.lookup = tt.lookup
			var verified []string
			service.verify = func(encoded, password string) (bool, error) {
				verified = append(verified, encoded)
				assert.Equal(t, "correct horse battery staple", password, "verified password")
				return false, nil
			}

			_, err := service.Login(context.Background(), "ada@example.com", "correct horse battery staple")

			assert.ErrorIs(t, err, ErrInvalidCredentials, "Login() error")
			assert.Equal(t, []string{tt.wantEncoded}, verified, "password verifications")
		})
	}
}

func TestDummyPasswordHashUsesProductionParameters(t *testing.T) {
	params, salt, key, err := parsePasswordHash(dummyPasswordHash)
	require.NoError(t, err, "parsePasswordHash(dummyPasswordHash)")
	assert.Equal(t, argonMemory, params.memory, "memory")
	assert.Equal(t, argonIterations, params.iterations, "iterations")
	assert.Equal(t, argonParallelism, params.parallelism, "parallelism")
	assert.Len(t, salt, argonSaltLength, "salt length")
	assert.Len(t, key, argonKeyLength, "key length")

	matches, err := VerifyPassword(dummyPasswordHash, "not the dummy password")
	require.NoError(t, err, "VerifyPassword(dummyPasswordHash)")
	assert.False(t, matches, "dummy hash matched an arbitrary password")
}

func TestMaybeDeleteExpiredSessionsRunsAtMostHourly(t *testing.T) {
	clock := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	var calls atomic.Int32
	service := NewService(nil)
	service.now = func() time.Time { return clock }
	service.cleanup = func(context.Context) (int64, error) {
		calls.Add(1)
		return 0, nil
	}

	service.maybeDeleteExpiredSessions(context.Background())
	require.Equal(t, int32(1), calls.Load(), "cleanup calls")

	clock = clock.Add(59 * time.Minute)
	service.maybeDeleteExpiredSessions(context.Background())
	require.Equal(t, int32(1), calls.Load(), "cleanup calls after an hour has not elapsed")

	clock = clock.Add(2 * time.Minute)
	service.maybeDeleteExpiredSessions(context.Background())
	assert.Equal(t, int32(2), calls.Load(), "cleanup calls after an hour elapsed")
}

func TestMaybeDeleteExpiredSessionsIsConcurrencySafe(t *testing.T) {
	clock := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	var calls atomic.Int32
	service := NewService(nil)
	service.now = func() time.Time { return clock }
	service.cleanup = func(context.Context) (int64, error) {
		calls.Add(1)
		return 0, nil
	}

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			service.maybeDeleteExpiredSessions(context.Background())
		}()
	}
	wg.Wait()

	assert.Equal(t, int32(1), calls.Load(), "concurrent cleanup calls")
}

func TestMaybeDeleteExpiredSessionsLogsFailureWithoutFailing(t *testing.T) {
	service := NewService(nil)
	service.cleanup = func(context.Context) (int64, error) {
		return 0, errors.New("database unavailable")
	}

	service.maybeDeleteExpiredSessions(context.Background())
}
