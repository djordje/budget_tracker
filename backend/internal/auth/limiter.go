package auth

import (
	"sync"
	"time"
)

const (
	limiterMaxKeys             = 10_000
	limiterMaintenanceInterval = time.Minute

	// attemptLimit and attemptWindow are the shared sign-in and registration
	// throttle policy: five attempts per fifteen minutes.
	attemptLimit  = 5
	attemptWindow = 15 * time.Minute
)

// AttemptLimiter is a small in-process throttle for authentication attempts. It
// is sufficient for the single-instance MVP; multi-instance deployments should
// replace it with a shared limiter.
type AttemptLimiter struct {
	mu                  sync.Mutex
	attempts            map[string][]time.Time
	max                 int
	window              time.Duration
	now                 func() time.Time
	maxKeys             int
	maintenanceInterval time.Duration
	lastMaintenance     time.Time
}

// NewLoginLimiter allows five sign-in attempts per fifteen minutes.
func NewLoginLimiter() *AttemptLimiter {
	return newAttemptLimiter(attemptLimit, attemptWindow, time.Now)
}

// NewRegistrationLimiter allows five registration attempts per fifteen minutes.
func NewRegistrationLimiter() *AttemptLimiter {
	return newAttemptLimiter(attemptLimit, attemptWindow, time.Now)
}

func newAttemptLimiter(max int, window time.Duration, now func() time.Time) *AttemptLimiter {
	return &AttemptLimiter{
		attempts:            make(map[string][]time.Time),
		max:                 max,
		window:              window,
		now:                 now,
		maxKeys:             limiterMaxKeys,
		maintenanceInterval: limiterMaintenanceInterval,
	}
}

// TryAcquire atomically reserves one attempt. Every admitted attempt consumes
// capacity immediately; call Reset after a successful login.
func (l *AttemptLimiter) TryAcquire(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.maintain(now)
	l.pruneKey(key, now)
	attempts := l.attempts[key]
	if len(attempts) >= l.max {
		return false
	}
	l.attempts[key] = append(attempts, now)
	l.enforceCap()
	return true
}

// Reset clears failures after a successful login.
func (l *AttemptLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

func (l *AttemptLimiter) pruneKey(key string, now time.Time) {
	cutoff := now.Add(-l.window)
	attempts := l.attempts[key]
	first := 0
	for first < len(attempts) && !attempts[first].After(cutoff) {
		first++
	}
	if first == len(attempts) {
		delete(l.attempts, key)
		return
	}
	if first > 0 {
		l.attempts[key] = append([]time.Time(nil), attempts[first:]...)
	}
}

// maintain prunes idle keys at a bounded frequency and enforces the hard key
// cap by evicting the key with the oldest most-recent attempt.
func (l *AttemptLimiter) maintain(now time.Time) {
	if !l.lastMaintenance.IsZero() && now.Sub(l.lastMaintenance) < l.maintenanceInterval {
		return
	}
	l.lastMaintenance = now

	cutoff := now.Add(-l.window)
	for key, attempts := range l.attempts {
		first := 0
		for first < len(attempts) && !attempts[first].After(cutoff) {
			first++
		}
		if first == len(attempts) {
			delete(l.attempts, key)
			continue
		}
		if first > 0 {
			l.attempts[key] = append([]time.Time(nil), attempts[first:]...)
		}
	}

	l.enforceCap()
}

func (l *AttemptLimiter) enforceCap() {
	for len(l.attempts) > l.maxKeys {
		var oldestKey string
		var oldest time.Time
		for key, attempts := range l.attempts {
			last := attempts[len(attempts)-1]
			if oldestKey == "" || last.Before(oldest) {
				oldestKey, oldest = key, last
			}
		}
		delete(l.attempts, oldestKey)
	}
}
