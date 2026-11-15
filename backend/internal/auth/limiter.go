package auth

import (
	"sync"
	"time"
)

const (
	// limiterMaxKeys bounds how many attempt keys the limiter tracks. New keys
	// are refused at the cap instead of evicting a live counter.
	limiterMaxKeys = 10_000

	// limiterMaxKeysPerAdmission bounds how many distinct attempt keys one
	// admission key (one client IP) may create per window. Junk keys therefore
	// cannot exhaust the shared cap and reset another client's counter.
	limiterMaxKeysPerAdmission = 10

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
	mu       sync.Mutex
	keys     map[string]attempt
	admitted map[string][]admittedKey
	max      int
	window   time.Duration
	now      func() time.Time

	maxKeys             int
	maxKeysPerAdmission int
	maintenanceInterval time.Duration
	lastMaintenance     time.Time
}

// attempt is one throttle bucket. Times stay sorted by admission order.
type attempt struct {
	admissionKey string
	times        []time.Time
}

// admittedKey records that an admission key created a bucket and when, so the
// admission budget expires with the window.
type admittedKey struct {
	key     string
	created time.Time
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
		keys:                make(map[string]attempt),
		admitted:            make(map[string][]admittedKey),
		max:                 max,
		window:              window,
		now:                 now,
		maxKeys:             limiterMaxKeys,
		maxKeysPerAdmission: limiterMaxKeysPerAdmission,
		maintenanceInterval: limiterMaintenanceInterval,
	}
}

// TryAcquire atomically reserves one attempt for key. admissionKey identifies
// the client that wants the bucket: creating a new bucket spends one of that
// client's per-window buckets, while a bucket that already exists keeps working
// even when the limiter is at its key cap. Every admitted attempt consumes
// capacity immediately; call Reset after a successful login.
func (l *AttemptLimiter) TryAcquire(key, admissionKey string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.maintain(now)
	l.pruneKey(key, now)

	record, exists := l.keys[key]
	if !exists {
		if len(l.keys) >= l.maxKeys {
			return false
		}
		if !l.admit(admissionKey, key, now) {
			return false
		}
		record = attempt{admissionKey: admissionKey}
	}
	if len(record.times) >= l.max {
		return false
	}
	record.times = append(record.times, now)
	l.keys[key] = record
	return true
}

// Reset clears the attempts for key after a successful attempt.
func (l *AttemptLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.deleteKey(key)
}

// pruneKey drops attempts outside the window and removes the bucket once it is
// empty, which also returns its admission budget.
func (l *AttemptLimiter) pruneKey(key string, now time.Time) {
	record, ok := l.keys[key]
	if !ok {
		return
	}
	record.times = pruneTimes(record.times, now.Add(-l.window))
	if len(record.times) == 0 {
		l.deleteKey(key)
		return
	}
	l.keys[key] = record
}

// deleteKey removes one bucket together with its admission record.
func (l *AttemptLimiter) deleteKey(key string) {
	record, ok := l.keys[key]
	if !ok {
		return
	}
	delete(l.keys, key)

	admitted := l.admitted[record.admissionKey]
	kept := make([]admittedKey, 0, len(admitted))
	for _, entry := range admitted {
		if entry.key != key {
			kept = append(kept, entry)
		}
	}
	if len(kept) == 0 {
		delete(l.admitted, record.admissionKey)
		return
	}
	l.admitted[record.admissionKey] = kept
}

// admit spends one bucket of admissionKey's per-window budget on a new key. It
// reports false once that budget is exhausted.
func (l *AttemptLimiter) admit(admissionKey, key string, now time.Time) bool {
	admitted := pruneAdmitted(l.admitted[admissionKey], now.Add(-l.window))
	if len(admitted) >= l.maxKeysPerAdmission {
		l.admitted[admissionKey] = admitted
		return false
	}
	l.admitted[admissionKey] = append(admitted, admittedKey{key: key, created: now})
	return true
}

// maintain prunes idle buckets and expired admission budgets at a bounded
// frequency so both maps stay bounded. It never drops a live bucket.
func (l *AttemptLimiter) maintain(now time.Time) {
	if !l.lastMaintenance.IsZero() && now.Sub(l.lastMaintenance) < l.maintenanceInterval {
		return
	}
	l.lastMaintenance = now

	for key := range l.keys {
		l.pruneKey(key, now)
	}
	for admissionKey, admitted := range l.admitted {
		kept := pruneAdmitted(admitted, now.Add(-l.window))
		if len(kept) == 0 {
			delete(l.admitted, admissionKey)
			continue
		}
		l.admitted[admissionKey] = kept
	}
}

// pruneTimes returns the attempts after cutoff.
func pruneTimes(times []time.Time, cutoff time.Time) []time.Time {
	first := 0
	for first < len(times) && !times[first].After(cutoff) {
		first++
	}
	if first == 0 {
		return times
	}
	return append([]time.Time(nil), times[first:]...)
}

// pruneAdmitted returns the admission records after cutoff.
func pruneAdmitted(admitted []admittedKey, cutoff time.Time) []admittedKey {
	kept := make([]admittedKey, 0, len(admitted))
	for _, entry := range admitted {
		if entry.created.After(cutoff) {
			kept = append(kept, entry)
		}
	}
	return kept
}
