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
