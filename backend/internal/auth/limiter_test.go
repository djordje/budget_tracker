package auth

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"time"
)

func TestRegistrationLimiterAllowsFiveAttemptsPerWindow(t *testing.T) {
	limiter := NewRegistrationLimiter()
	assert.Equal(t, 5, limiter.max, "registration attempt limit")
	assert.Equal(t, 15*time.Minute, limiter.window, "registration attempt window")

	const key = "client|email"
	for i := 0; i < 5; i++ {
		assert.True(t, limiter.TryAcquire(key, "client"), "attempt %d", i+1)
	}
	assert.False(t, limiter.TryAcquire(key, "client"), "sixth attempt")
}

func TestAttemptLimiterReservesAtomicallyAndExpires(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	limiter := newAttemptLimiter(2, time.Minute, func() time.Time { return now })
	const key = "hashed-key"

	assert.True(t, limiter.TryAcquire(key, "client"), "first fresh attempt")
	assert.True(t, limiter.TryAcquire(key, "client"), "second fresh attempt")
	assert.False(t, limiter.TryAcquire(key, "client"), "limit was not enforced")
	assert.True(t, limiter.TryAcquire("other", "client"), "one key affected another key")

	now = now.Add(61 * time.Second)
	assert.True(t, limiter.TryAcquire(key, "client"), "attempts did not expire")
	limiter.Reset(key)
	assert.True(t, limiter.TryAcquire(key, "client"), "reset did not clear attempts")
}

func TestAttemptLimiterConcurrentAdmissions(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	limiter := newAttemptLimiter(5, 15*time.Minute, func() time.Time { return now })

	var admitted atomic.Int32
	var wg sync.WaitGroup
	for range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if limiter.TryAcquire("same-key", "same-client") {
				admitted.Add(1)
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, int32(5), admitted.Load(), "admitted attempts")
}

func TestAttemptLimiterChurnCannotResetLiveCounter(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	limiter := newAttemptLimiter(2, 15*time.Minute, func() time.Time { return now })
	limiter.maxKeys = 6
	limiter.maxKeysPerAdmission = 6
	limiter.maintenanceInterval = 0

	assert.True(t, limiter.TryAcquire("victim", "victim-ip"), "first victim attempt")
	assert.True(t, limiter.TryAcquire("victim", "victim-ip"), "second victim attempt")

	for i := 0; i < 5; i++ {
		now = now.Add(time.Second)
		require.True(t, limiter.TryAcquire(fmt.Sprintf("junk-%d", i), "attacker-ip"), "junk key %d", i)
	}

	assert.False(t, limiter.TryAcquire("junk-5", "attacker-ip"), "new key at the cap")
	assert.Contains(t, limiter.keys, "victim", "victim counter was evicted")
	assert.False(t, limiter.TryAcquire("victim", "victim-ip"), "victim limit was reset by churn")
}

func TestAttemptLimiterBoundsKeysPerAdmission(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	limiter := newAttemptLimiter(5, 15*time.Minute, func() time.Time { return now })
	limiter.maxKeysPerAdmission = 3
	limiter.maintenanceInterval = 0

	for i := 0; i < 3; i++ {
		assert.True(t, limiter.TryAcquire(fmt.Sprintf("key-%d", i), "client"), "key %d", i)
	}
	assert.False(t, limiter.TryAcquire("one-too-many", "client"), "admission budget was not enforced")
	assert.True(t, limiter.TryAcquire("key-0", "client"), "existing key refused after the admission budget")
	assert.True(t, limiter.TryAcquire("other-client-key", "other-client"), "one admission key affected another")
}

func TestAttemptLimiterKeepsExistingKeysAtCapacity(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	limiter := newAttemptLimiter(5, 15*time.Minute, func() time.Time { return now })
	limiter.maxKeys = 2
	limiter.maintenanceInterval = 0

	assert.True(t, limiter.TryAcquire("first", "client-a"), "first key")
	assert.True(t, limiter.TryAcquire("second", "client-b"), "second key")
	assert.False(t, limiter.TryAcquire("new", "client-c"), "new key at the cap")
	assert.True(t, limiter.TryAcquire("first", "client-a"), "existing key at the cap")
}

func TestAttemptLimiterMaintenanceReclaimsExpiredKeys(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	limiter := newAttemptLimiter(5, 15*time.Minute, func() time.Time { return now })
	limiter.maxKeys = 2
	limiter.maxKeysPerAdmission = 1
	limiter.maintenanceInterval = 0

	assert.True(t, limiter.TryAcquire("old", "client-a"), "first key")
	assert.True(t, limiter.TryAcquire("newer", "client-b"), "second key")
	assert.False(t, limiter.TryAcquire("third", "client-c"), "new key at the cap")

	now = now.Add(16 * time.Minute)
	assert.True(t, limiter.TryAcquire("third", "client-c"), "expired keys did not free capacity")
	assert.Len(t, limiter.keys, 1, "keys after maintenance")
	assert.Len(t, limiter.admitted, 1, "admission budgets after maintenance")
}

func TestAttemptLimiterPolicyBounds(t *testing.T) {
	login := NewLoginLimiter()
	assert.Equal(t, 10_000, login.maxKeys, "login key cap")
	assert.Equal(t, 50, login.maxKeysPerAdmission, "login admission budget")

	registration := NewRegistrationLimiter()
	assert.Equal(t, login.maxKeys, registration.maxKeys, "registration key cap")
	assert.Equal(t, login.maxKeysPerAdmission, registration.maxKeysPerAdmission, "registration admission budget")
}

func TestAttemptLimiterAdmissionBudgetRecoversAfterWindow(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	limiter := newAttemptLimiter(5, 15*time.Minute, func() time.Time { return now })
	limiter.maxKeysPerAdmission = 1
	limiter.maintenanceInterval = 0

	require.True(t, limiter.TryAcquire("first", "client"), "first bucket")
	require.False(t, limiter.TryAcquire("second", "client"), "admission budget was not enforced")

	now = now.Add(16 * time.Minute)
	assert.True(t, limiter.TryAcquire("second", "client"), "admission budget did not recover after the window")
}
