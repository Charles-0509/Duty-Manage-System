package middleware

import (
	"sync"
	"time"
)

// attemptWindow counts failures inside a window and blocks the key once the
// failure budget is exhausted. Successful logins reset the counter.
type attemptWindow struct {
	failures  int
	windowEnd time.Time
	blockEnd  time.Time
}

// RateLimiter is a small in-memory brute-force guard. One instance is shared
// by the login and refresh endpoints.
type RateLimiter struct {
	mu          sync.Mutex
	entries     map[string]*attemptWindow
	maxFail     int
	windowSize  time.Duration
	blockSize   time.Duration
	nextCleanup time.Time
}

const rateLimiterMaxEntries = 10000

func NewRateLimiter(maxFailures int, window, block time.Duration) *RateLimiter {
	return &RateLimiter{
		entries:    map[string]*attemptWindow{},
		maxFail:    maxFailures,
		windowSize: window,
		blockSize:  block,
	}
}

func (l *RateLimiter) window(key string) *attemptWindow {
	now := time.Now()
	entry, ok := l.entries[key]
	if !ok && len(l.entries) >= rateLimiterMaxEntries {
		l.cleanupExpired(now)
		if len(l.entries) >= rateLimiterMaxEntries {
			for oldestKey := range l.entries {
				delete(l.entries, oldestKey)
				break
			}
		}
	}
	if !ok || now.After(entry.windowEnd) {
		entry = &attemptWindow{windowEnd: now.Add(l.windowSize)}
		l.entries[key] = entry
	}
	return entry
}

func (l *RateLimiter) cleanupExpired(now time.Time) {
	if now.Before(l.nextCleanup) {
		return
	}
	for key, entry := range l.entries {
		expiry := entry.windowEnd
		if entry.blockEnd.After(expiry) {
			expiry = entry.blockEnd
		}
		if now.After(expiry) {
			delete(l.entries, key)
		}
	}
	l.nextCleanup = now.Add(time.Minute)
}

// Allow reports whether the key may attempt again. When false the key is
// temporarily blocked and the remaining seconds are returned.
func (l *RateLimiter) Allow(key string) (bool, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry := l.window(key)
	if time.Now().Before(entry.blockEnd) {
		return false, int(time.Until(entry.blockEnd).Seconds()) + 1
	}
	return true, 0
}

// RecordFailure registers a failed attempt; enough failures inside the window
// block the key for the block duration.
func (l *RateLimiter) RecordFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry := l.window(key)
	entry.failures++
	if entry.failures >= l.maxFail {
		entry.blockEnd = time.Now().Add(l.blockSize)
		entry.failures = 0
		entry.windowEnd = time.Now().Add(l.windowSize)
	}
}

// RecordSuccess clears failure history for the key.
func (l *RateLimiter) RecordSuccess(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}
