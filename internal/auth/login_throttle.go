package auth

import (
	"net"
	"strings"
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

	// Per-account limits apply across every source address, so rotating
	// IPs does not grant unlimited guesses. The threshold is high and the
	// lockout short so a third party cannot cheaply lock a real account
	// out of the panel.
	accountMaxFailures = 50
	accountLockout     = 5 * time.Minute

	// After this many failures from one address, or against one account,
	// the login form requires a captcha. Counters use their own keys so
	// they never lock anyone out by themselves.
	captchaAfterFailures = 2
	captchaWindow        = 15 * time.Minute
)

// captchaHostKey counts failures from one address across all usernames.
func captchaHostKey(remoteAddr string) string {
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	return "captcha|ip|" + host
}

// captchaAccountKey counts failures against one account from anywhere.
func captchaAccountKey(username string) string {
	return "captcha|account|" + username
}

// throttleKey identifies one client/account pair. The source port changes
// per TCP connection, so only the host is used.
func throttleKey(remoteAddr, username string) string {
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	return "ip|" + host + "|" + username
}

// accountKey identifies an account across all source addresses.
func accountKey(username string) string {
	return "account|" + username
}

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

	maxFailures, lockout := limitsFor(key)
	t.pruneLocked(key, now.Add(-lockout))
	fails := t.failures[key]
	if len(fails) < maxFailures {
		return false, 0
	}
	remaining := lockout - now.Sub(fails[len(fails)-1])
	if remaining <= 0 {
		return false, 0
	}
	return true, remaining
}

// recordFailure notes a failed attempt for key.
func (t *loginThrottle) recordFailure(key string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	_, lockout := limitsFor(key)
	t.pruneLocked(key, now.Add(-(loginFailureWindow + lockout)))
	t.failures[key] = append(t.failures[key], now)
}

// reset clears the failure history for key after a successful login.
func (t *loginThrottle) reset(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.failures, key)
}

// count returns how many failures key has within the captcha window.
func (t *loginThrottle) count(key string, now time.Time) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pruneLocked(key, now.Add(-captchaWindow))
	return len(t.failures[key])
}

// limitsFor returns the threshold and lockout that apply to a key.
func limitsFor(key string) (int, time.Duration) {
	if strings.HasPrefix(key, "captcha|") {
		return captchaAfterFailures, captchaWindow
	}
	if strings.HasPrefix(key, "account|") {
		return accountMaxFailures, accountLockout
	}
	return loginMaxFailures, loginLockout
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
