package middleware

import (
	"sync"
	"time"
)

// attemptWindow counts failures inside a window and blocks the key once the
// failure budget is exhausted. Successful logins reset the counter.
type attemptWindow struct {
	failures  int
	blocks    int
	windowEnd time.Time
	blockEnd  time.Time
	accounts  map[string]struct{}
}

type FailureState struct {
	RemainingAttempts int
	RetryAfterSeconds int
	Blocked           bool
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
	if !ok {
		entry = &attemptWindow{windowEnd: now.Add(l.windowSize), accounts: map[string]struct{}{}}
		l.entries[key] = entry
	} else if now.After(entry.windowEnd) {
		entry.failures = 0
		entry.windowEnd = now.Add(l.windowSize)
	}
	return entry
}

func (l *RateLimiter) cleanupExpired(now time.Time) {
	if now.Before(l.nextCleanup) {
		return
	}
	for key, entry := range l.entries {
		if entry.blocks == 0 && now.After(entry.windowEnd) {
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

// RecordFailure registers a failed attempt. The first block starts after the
// failure budget is exhausted; every later failure after an expired block
// immediately doubles the previous block duration.
func (l *RateLimiter) RecordFailure(key string) FailureState {
	return l.RecordFailureFor(key, "")
}

func (l *RateLimiter) RecordFailureFor(key, account string) FailureState {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry := l.window(key)
	if account != "" {
		entry.accounts[account] = struct{}{}
	}
	if entry.blocks > 0 {
		return l.block(entry)
	}
	entry.failures++
	if entry.failures >= l.maxFail {
		return l.block(entry)
	}
	return FailureState{RemainingAttempts: l.maxFail - entry.failures}
}

func (l *RateLimiter) block(entry *attemptWindow) FailureState {
	entry.blocks++
	duration := l.blockSize
	const maxDuration = time.Duration(1<<63 - 1)
	for block := 1; block < entry.blocks; block++ {
		if duration > maxDuration/2 {
			duration = maxDuration
			break
		}
		duration *= 2
	}
	now := time.Now()
	entry.blockEnd = now.Add(duration)
	entry.windowEnd = entry.blockEnd
	entry.failures = 0
	return FailureState{
		RetryAfterSeconds: int(duration / time.Second),
		Blocked:           true,
	}
}

// RecordSuccess clears failure history for the key.
func (l *RateLimiter) RecordSuccess(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

// ResetAccount clears every device/IP entry that recorded a failure for an
// account. This lets an administrator's password reset unlock it immediately.
func (l *RateLimiter) ResetAccount(account string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for key, entry := range l.entries {
		if _, ok := entry.accounts[account]; ok {
			delete(l.entries, key)
		}
	}
}
