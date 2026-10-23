package auth

import (
	"testing"
	"time"
)

func TestLoginThrottleLocksAfterMaxFailures(t *testing.T) {
	now := time.Now()
	th := newLoginThrottle()
	key := "1.2.3.4|admin"

	for i := 0; i < loginMaxFailures; i++ {
		if blocked, _ := th.blocked(key, now); blocked {
			t.Fatalf("attempt %d must not be blocked yet", i+1)
		}
		th.recordFailure(key, now)
	}

	blocked, remaining := th.blocked(key, now)
	if !blocked {
		t.Fatal("key must be locked after max failures")
	}
	if remaining <= 0 || remaining > loginLockout {
		t.Fatalf("remaining = %v, want within lockout window", remaining)
	}

	// A different key is unaffected.
	if blocked, _ := th.blocked("5.6.7.8|admin", now); blocked {
		t.Error("other keys must not be blocked")
	}
}

func TestLoginThrottleResetAfterSuccess(t *testing.T) {
	now := time.Now()
	th := newLoginThrottle()
	key := "1.2.3.4|admin"

	for i := 0; i < loginMaxFailures-1; i++ {
		th.recordFailure(key, now)
	}
	th.reset(key)

	if blocked, _ := th.blocked(key, now); blocked {
		t.Error("history must be cleared after reset")
	}
}

func TestLoginThrottleOldFailuresExpire(t *testing.T) {
	th := newLoginThrottle()
	key := "1.2.3.4|admin"
	old := time.Now().Add(-2 * loginLockout)

	for i := 0; i < loginMaxFailures; i++ {
		th.recordFailure(key, old)
	}

	if blocked, _ := th.blocked(key, time.Now()); blocked {
		t.Error("failures older than the window must not lock the key")
	}
}

func TestThrottleKeyIgnoresTheSourcePort(t *testing.T) {
	// Each TCP connection has a new source port; the throttle must still
	// see the attempts as coming from one client.
	first := throttleKey("203.0.113.9:51514", "admin")
	second := throttleKey("203.0.113.9:51515", "admin")
	if first != second {
		t.Fatalf("keys differ across connections: %q vs %q", first, second)
	}
	if throttleKey("203.0.113.9:51514", "admin") == throttleKey("198.51.100.4:51514", "admin") {
		t.Error("different clients must not share a key")
	}
	if throttleKey("203.0.113.9:51514", "admin") == throttleKey("203.0.113.9:51514", "other") {
		t.Error("different accounts must not share a key")
	}
	// A bare host (set by the proxy middleware) works too.
	if throttleKey("203.0.113.9", "admin") != first {
		t.Error("a bare host must produce the same key as host:port")
	}
}

func TestAccountThrottleLimitsAttemptsFromManySources(t *testing.T) {
	throttle := newLoginThrottle()
	now := time.Now()
	account := accountKey("admin")

	// Attempts spread over many source addresses still count against the
	// account, so rotating IPs does not buy unlimited guesses.
	for i := 0; i < accountMaxFailures; i++ {
		if blocked, _ := throttle.blocked(account, now); blocked {
			t.Fatalf("account locked out after %d attempts, limit is %d", i, accountMaxFailures)
		}
		throttle.recordFailure(account, now)
	}
	blocked, remaining := throttle.blocked(account, now)
	if !blocked || remaining <= 0 {
		t.Fatal("the account was not locked out after reaching the limit")
	}
	// A different account is unaffected.
	if blocked, _ := throttle.blocked(accountKey("someone-else"), now); blocked {
		t.Error("locking one account must not lock others")
	}
	// The lockout expires.
	if blocked, _ := throttle.blocked(account, now.Add(accountLockout+time.Second)); blocked {
		t.Error("the account lockout did not expire")
	}
}
