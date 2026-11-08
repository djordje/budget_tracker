package auth

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoginLimiterReservesAtomicallyAndExpires(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	limiter := newLoginLimiter(2, time.Minute, func() time.Time { return now })
	const key = "hashed-key"

	if !limiter.TryAcquire(key) || !limiter.TryAcquire(key) {
		t.Fatal("fresh limiter did not admit the allowed attempts")
	}
	if limiter.TryAcquire(key) {
		t.Fatal("limit was not enforced")
	}
	if !limiter.TryAcquire("other") {
		t.Fatal("one key affected another key")
	}

	now = now.Add(61 * time.Second)
	if !limiter.TryAcquire(key) {
		t.Fatal("attempts did not expire")
	}
	limiter.Reset(key)
	if !limiter.TryAcquire(key) {
		t.Fatal("reset did not clear attempts")
	}
}

func TestLoginLimiterConcurrentAdmissions(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	limiter := newLoginLimiter(5, 15*time.Minute, func() time.Time { return now })

	var admitted atomic.Int32
	var wg sync.WaitGroup
	for range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if limiter.TryAcquire("same-key") {
				admitted.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := admitted.Load(); got != 5 {
		t.Errorf("admitted attempts = %d, want 5", got)
	}
}

func TestLoginLimiterMaintainsBoundsAndEvictsOldest(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	limiter := newLoginLimiter(5, 15*time.Minute, func() time.Time { return now })
	limiter.maxKeys = 2
	limiter.maintenanceInterval = 0

	limiter.TryAcquire("old")
	now = now.Add(time.Second)
	limiter.TryAcquire("newer")
	now = now.Add(time.Second)
	limiter.TryAcquire("newest")

	if len(limiter.attempts) > 2 {
		t.Errorf("limiter holds %d keys, want at most 2", len(limiter.attempts))
	}
	if _, ok := limiter.attempts["old"]; ok {
		t.Error("oldest key was not evicted")
	}
	if _, ok := limiter.attempts["newest"]; !ok {
		t.Error("newest key was evicted")
	}

	now = now.Add(16 * time.Minute)
	limiter.TryAcquire("fresh")
	if len(limiter.attempts) != 1 {
		t.Errorf("idle keys were not pruned: %d keys remain", len(limiter.attempts))
	}
}
