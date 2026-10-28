package dispatch_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"clawbridge/pkg/dispatch"
)

func TestTokenBucketLimiter_BurstCapacityAndExhaustion(t *testing.T) {
	limiter := dispatch.NewTokenBucketLimiter(map[string]dispatch.ChannelRate{
		"slack": {RatePerSecond: 1.0, Capacity: 3.0},
	})

	// First 3 immediate acquires should succeed using burst capacity
	for i := 0; i < 3; i++ {
		allowed, retryAfter := limiter.Acquire("slack")
		if !allowed {
			t.Fatalf("expected acquire %d to be allowed", i)
		}
		if retryAfter != 0 {
			t.Fatalf("expected 0 retryAfter, got %v", retryAfter)
		}
	}

	// 4th immediate acquire should be rejected
	allowed, retryAfter := limiter.Acquire("slack")
	if allowed {
		t.Fatalf("expected 4th acquire to be rejected due to rate limit")
	}
	if retryAfter <= 0 {
		t.Fatalf("expected positive retryAfter, got %v", retryAfter)
	}
}

func TestTokenBucketLimiter_RefillOverTime(t *testing.T) {
	limiter := dispatch.NewTokenBucketLimiter(map[string]dispatch.ChannelRate{
		"fast": {RatePerSecond: 20.0, Capacity: 2.0},
	})

	limiter.Acquire("fast")
	limiter.Acquire("fast")

	allowed, _ := limiter.Acquire("fast")
	if allowed {
		t.Fatalf("expected bucket to be empty")
	}

	// Sleep 100ms (refills 20.0 * 0.1 = 2 tokens)
	time.Sleep(110 * time.Millisecond)

	allowed, _ = limiter.Acquire("fast")
	if !allowed {
		t.Fatalf("expected token to refill and acquire to succeed")
	}
}

func TestTokenBucketLimiter_RecordThrottleBlocksAcquisition(t *testing.T) {
	limiter := dispatch.NewTokenBucketLimiter(nil)

	limiter.RecordThrottle("twitter", 200*time.Millisecond)

	if limiter.IsAvailable("twitter") {
		t.Fatalf("expected twitter to be throttled")
	}

	allowed, retryAfter := limiter.Acquire("twitter")
	if allowed {
		t.Fatalf("expected acquire to be blocked during throttle window")
	}
	if retryAfter <= 0 {
		t.Fatalf("expected positive retryAfter during throttle")
	}

	time.Sleep(210 * time.Millisecond)

	if !limiter.IsAvailable("twitter") {
		t.Fatalf("expected twitter to be available after throttle window elapsed")
	}
}

func TestTokenBucketLimiter_ConcurrentThreadSafety(t *testing.T) {
	limiter := dispatch.NewTokenBucketLimiter(map[string]dispatch.ChannelRate{
		"jira": {RatePerSecond: 100.0, Capacity: 10.0},
	})

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)

	var allowedCount int64

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			allowed, _ := limiter.Acquire("jira")
			if allowed {
				atomic.AddInt64(&allowedCount, 1)
			}
		}()
	}

	wg.Wait()

	// Initial burst capacity was 10.0, so at most 10-12 should have been allowed in this microsecond burst
	count := atomic.LoadInt64(&allowedCount)
	if count > 12 {
		t.Fatalf("expected at most burst capacity allowed (~10-12), got %d", count)
	}
}
