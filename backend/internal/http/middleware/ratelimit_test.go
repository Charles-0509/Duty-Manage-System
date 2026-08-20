package middleware

import (
	"fmt"
	"testing"
	"time"
)

func TestRateLimiterBlocksAfterFailures(t *testing.T) {
	limiter := NewRateLimiter(3, 50*time.Millisecond, 40*time.Millisecond)

	for i := 0; i < 3; i++ {
		if allowed, _ := limiter.Allow("ip:1.2.3.4"); !allowed {
			t.Fatalf("attempt %d blocked too early", i+1)
		}
		limiter.RecordFailure("ip:1.2.3.4")
	}

	if allowed, _ := limiter.Allow("ip:1.2.3.4"); allowed {
		t.Fatal("key not blocked after failure budget exhausted")
	}
	if allowed, _ := limiter.Allow("ip:5.6.7.8"); !allowed {
		t.Fatal("other key affected by unrelated failures")
	}

	time.Sleep(45 * time.Millisecond)
	if allowed, _ := limiter.Allow("ip:1.2.3.4"); !allowed {
		t.Fatal("key still blocked after block window expired")
	}
}

func TestRateLimiterBoundsAttackerControlledKeys(t *testing.T) {
	limiter := NewRateLimiter(3, time.Minute, time.Minute)
	for index := 0; index < rateLimiterMaxEntries+50; index++ {
		limiter.Allow(fmt.Sprintf("user:%d", index))
	}
	if len(limiter.entries) > rateLimiterMaxEntries {
		t.Fatalf("entries=%d, want <= %d", len(limiter.entries), rateLimiterMaxEntries)
	}
	for range 3 {
		limiter.RecordFailure("new-client-after-cap")
	}
	if allowed, _ := limiter.Allow("new-client-after-cap"); allowed {
		t.Fatal("new client was not tracked after the limiter reached capacity")
	}
}

func TestRateLimiterSuccessResetsFailures(t *testing.T) {
	limiter := NewRateLimiter(3, 100*time.Millisecond, 40*time.Millisecond)

	limiter.RecordFailure("user:abc")
	limiter.RecordFailure("user:abc")
	limiter.RecordSuccess("user:abc")
	limiter.RecordFailure("user:abc")
	limiter.RecordFailure("user:abc")

	if allowed, _ := limiter.Allow("user:abc"); !allowed {
		t.Fatal("key blocked despite success reset")
	}
}

func TestRateLimiterDoublesEveryLaterBlock(t *testing.T) {
	limiter := NewRateLimiter(2, time.Minute, 5*time.Minute)

	first := limiter.RecordFailure("login:member:device")
	if first.Blocked || first.RemainingAttempts != 1 {
		t.Fatalf("first failure state=%+v", first)
	}
	initialBlock := limiter.RecordFailure("login:member:device")
	if !initialBlock.Blocked || initialBlock.RetryAfterSeconds < 300 {
		t.Fatalf("initial block state=%+v", initialBlock)
	}

	limiter.entries["login:member:device"].blockEnd = time.Now().Add(-time.Second)
	doubledBlock := limiter.RecordFailure("login:member:device")
	if !doubledBlock.Blocked || doubledBlock.RetryAfterSeconds < 600 {
		t.Fatalf("doubled block state=%+v", doubledBlock)
	}

	limiter.entries["login:member:device"].blockEnd = time.Now().Add(-time.Second)
	thirdBlock := limiter.RecordFailure("login:member:device")
	if !thirdBlock.Blocked || thirdBlock.RetryAfterSeconds < 1200 {
		t.Fatalf("third block state=%+v", thirdBlock)
	}
}

func TestRateLimiterResetAccountClearsAccountDevices(t *testing.T) {
	limiter := NewRateLimiter(1, time.Minute, time.Minute)
	limiter.RecordFailureFor("ip:one", "member")
	limiter.RecordFailureFor("device:member:two", "member")
	limiter.RecordFailureFor("device:other:three", "other")

	limiter.ResetAccount("member")

	if allowed, _ := limiter.Allow("ip:one"); !allowed {
		t.Fatal("member IP restriction was not reset")
	}
	if allowed, _ := limiter.Allow("device:member:two"); !allowed {
		t.Fatal("member device restriction was not reset")
	}
	if allowed, _ := limiter.Allow("device:other:three"); allowed {
		t.Fatal("unrelated account restriction was reset")
	}
}
