package auth

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"time"
)

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
