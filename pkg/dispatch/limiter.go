package dispatch

import (
	"math"
	"sync"
	"time"
)

// ChannelRate configures rate-per-second and burst capacity for a channel.
type ChannelRate struct {
	RatePerSecond float64
	Capacity      float64
}

type bucket struct {
	tokens        float64
	lastRefill    time.Time
	ratePerSecond float64
	capacity      float64
	throttleUntil time.Time
}

// TokenBucketLimiter tracks per-channel rate limits and downstream throttle windows.
type TokenBucketLimiter struct {
	mu           sync.Mutex
	defaultRates map[string]ChannelRate
	buckets      map[string]*bucket
}

// NewTokenBucketLimiter creates a new rate limiter with optional custom channel rates.
func NewTokenBucketLimiter(rates map[string]ChannelRate) *TokenBucketLimiter {
	defaults := map[string]ChannelRate{
		"slack":   {RatePerSecond: 1.0, Capacity: 3.0},
		"jira":    {RatePerSecond: 5.0, Capacity: 10.0},
		"twitter": {RatePerSecond: 2.0, Capacity: 5.0},
		"discord": {RatePerSecond: 5.0, Capacity: 10.0},
		"default": {RatePerSecond: 5.0, Capacity: 10.0},
	}

	for k, v := range rates {
		defaults[k] = v
	}

	return &TokenBucketLimiter{
		defaultRates: defaults,
		buckets:      make(map[string]*bucket),
	}
}

func (l *TokenBucketLimiter) getBucket(channel string) *bucket {
	b, exists := l.buckets[channel]
	if !exists {
		rateCfg, ok := l.defaultRates[channel]
		if !ok {
			rateCfg = l.defaultRates["default"]
		}
		b = &bucket{
			tokens:        rateCfg.Capacity,
			lastRefill:    time.Now(),
			ratePerSecond: rateCfg.RatePerSecond,
			capacity:      rateCfg.Capacity,
		}
		l.buckets[channel] = b
	}
	return b
}

func (b *bucket) refill(now time.Time) {
	elapsed := now.Sub(b.lastRefill).Seconds()
	if elapsed > 0 {
		b.tokens = math.Min(b.capacity, b.tokens+(elapsed*b.ratePerSecond))
		b.lastRefill = now
	}
}

// Acquire attempts to acquire 1 token for the specified channel.
// Returns allowed=true if token acquired, or allowed=false with estimated retryAfter.
func (l *TokenBucketLimiter) Acquire(channel string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	b := l.getBucket(channel)

	// Check if channel is currently under downstream HTTP 429 throttle
	if !b.throttleUntil.IsZero() && now.Before(b.throttleUntil) {
		return false, b.throttleUntil.Sub(now)
	}

	b.refill(now)

	if b.tokens >= 1.0 {
		b.tokens -= 1.0
		return true, 0
	}

	// Calculate wait time for 1 token to refill
	needed := 1.0 - b.tokens
	waitSec := needed / b.ratePerSecond
	return false, time.Duration(waitSec * float64(time.Second))
}

// RecordThrottle blocks a channel for the specified duration (e.g. following downstream 429).
func (l *TokenBucketLimiter) RecordThrottle(channel string, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	b := l.getBucket(channel)
	b.throttleUntil = time.Now().Add(retryAfter)
}

// IsAvailable checks whether requests to the channel are currently permitted without consuming a token.
func (l *TokenBucketLimiter) IsAvailable(channel string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	b := l.getBucket(channel)

	if !b.throttleUntil.IsZero() && now.Before(b.throttleUntil) {
		return false
	}

	b.refill(now)
	return b.tokens >= 1.0
}
