package auth

import (
	"sync"
	"time"
)

// Brute-force protection for the panel login: after too many failed attempts
// for the same source+username combination, further attempts are locked out
// for a cooldown period. State is in-memory — a panel restart clears the
// counters, which is acceptable because restarts do not help an attacker
// guess credentials any faster.
const (
	loginMaxFailures   = 5
	loginFailureWindow = 15 * time.Minute
	loginLockout       = 15 * time.Minute
)

// loginThrottle tracks failed login attempts per key.
type loginThrottle struct {
	mu       sync.Mutex
	failures map[string][]time.Time
}

func newLoginThrottle() *loginThrottle {
	return &loginThrottle{failures: make(map[string][]time.Time)}
}

// blocked reports whether key is locked out and for how much longer.
func (t *loginThrottle) blocked(key string, now time.Time) (bool, time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.pruneLocked(key, now.Add(-loginLockout))
	fails := t.failures[key]
	if len(fails) < loginMaxFailures {
		return false, 0
	}
	remaining := loginLockout - now.Sub(fails[len(fails)-1])
	if remaining <= 0 {
		return false, 0
	}
	return true, remaining
}

// recordFailure notes a failed attempt for key.
func (t *loginThrottle) recordFailure(key string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.pruneLocked(key, now.Add(-(loginFailureWindow + loginLockout)))
	t.failures[key] = append(t.failures[key], now)
}

// reset clears the failure history for key after a successful login.
func (t *loginThrottle) reset(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.failures, key)
}

// pruneLocked drops timestamps that can no longer influence the decision.
func (t *loginThrottle) pruneLocked(key string, cutoff time.Time) {
	kept := t.failures[key][:0]
	for _, ts := range t.failures[key] {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	if len(kept) == 0 {
		delete(t.failures, key)
		return
	}
	t.failures[key] = kept
}
