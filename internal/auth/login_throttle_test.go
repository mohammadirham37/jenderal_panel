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
