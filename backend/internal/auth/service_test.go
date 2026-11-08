package auth

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
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
	if got := calls.Load(); got != 1 {
		t.Fatalf("cleanup calls = %d, want 1", got)
	}

	clock = clock.Add(59 * time.Minute)
	service.maybeDeleteExpiredSessions(context.Background())
	if got := calls.Load(); got != 1 {
		t.Fatalf("cleanup calls = %d, want 1 after an hour has not elapsed", got)
	}

	clock = clock.Add(2 * time.Minute)
	service.maybeDeleteExpiredSessions(context.Background())
	if got := calls.Load(); got != 2 {
		t.Fatalf("cleanup calls = %d, want 2 after an hour elapsed", got)
	}
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

	if got := calls.Load(); got != 1 {
		t.Fatalf("concurrent cleanup calls = %d, want 1", got)
	}
}

func TestMaybeDeleteExpiredSessionsLogsFailureWithoutFailing(t *testing.T) {
	service := NewService(nil)
	service.cleanup = func(context.Context) (int64, error) {
		return 0, errors.New("database unavailable")
	}

	service.maybeDeleteExpiredSessions(context.Background())
}
