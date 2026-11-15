package auth

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"time"
)

func TestLoginLimiterReservesAtomicallyAndExpires(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	limiter := newLoginLimiter(2, time.Minute, func() time.Time { return now })
	const key = "hashed-key"

	assert.True(t, limiter.TryAcquire(key), "first fresh attempt")
	assert.True(t, limiter.TryAcquire(key), "second fresh attempt")
	assert.False(t, limiter.TryAcquire(key), "limit was not enforced")
	assert.True(t, limiter.TryAcquire("other"), "one key affected another key")

	now = now.Add(61 * time.Second)
	assert.True(t, limiter.TryAcquire(key), "attempts did not expire")
	limiter.Reset(key)
	assert.True(t, limiter.TryAcquire(key), "reset did not clear attempts")
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

	assert.Equal(t, int32(5), admitted.Load(), "admitted attempts")
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

	assert.LessOrEqual(t, len(limiter.attempts), 2, "limiter keys")
	assert.NotContains(t, limiter.attempts, "old", "oldest key was not evicted")
	assert.Contains(t, limiter.attempts, "newest", "newest key was evicted")

	now = now.Add(16 * time.Minute)
	limiter.TryAcquire("fresh")
	assert.Len(t, limiter.attempts, 1, "idle keys were not pruned")
}
