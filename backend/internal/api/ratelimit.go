package api

import (
	"sync"
	"time"
)

// RateLimiter enforces per-token sliding window rate limits.
type RateLimiter struct {
	mu      sync.Mutex
	windows map[string]*tokenWindow
}

type tokenWindow struct {
	readTimes  []time.Time
	writeTimes []time.Time
}

// NewRateLimiter creates a rate limiter and starts a background cleanup goroutine.
func NewRateLimiter() *RateLimiter {
	rl := &RateLimiter{
		windows: make(map[string]*tokenWindow),
	}
	go rl.cleanup()
	return rl
}

// Allow checks whether a request is permitted under the rate limit.
// Returns true if allowed, or false with the duration to wait before retrying.
func (rl *RateLimiter) Allow(tokenID string, isWrite bool, readLimit, writeLimit int) (bool, time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-time.Minute)

	w, ok := rl.windows[tokenID]
	if !ok {
		w = &tokenWindow{}
		rl.windows[tokenID] = w
	}

	if isWrite {
		w.writeTimes = pruneOld(w.writeTimes, cutoff)
		if len(w.writeTimes) >= writeLimit {
			retryAfter := w.writeTimes[0].Add(time.Minute).Sub(now)
			if retryAfter < time.Second {
				retryAfter = time.Second
			}
			return false, retryAfter
		}
		w.writeTimes = append(w.writeTimes, now)
	} else {
		w.readTimes = pruneOld(w.readTimes, cutoff)
		if len(w.readTimes) >= readLimit {
			retryAfter := w.readTimes[0].Add(time.Minute).Sub(now)
			if retryAfter < time.Second {
				retryAfter = time.Second
			}
			return false, retryAfter
		}
		w.readTimes = append(w.readTimes, now)
	}

	return true, 0
}

func pruneOld(times []time.Time, cutoff time.Time) []time.Time {
	i := 0
	for i < len(times) && times[i].Before(cutoff) {
		i++
	}
	if i > 0 {
		return times[i:]
	}
	return times
}

// cleanup runs every 5 minutes and removes stale entries.
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		cutoff := time.Now().Add(-time.Minute)
		for id, w := range rl.windows {
			w.readTimes = pruneOld(w.readTimes, cutoff)
			w.writeTimes = pruneOld(w.writeTimes, cutoff)
			if len(w.readTimes) == 0 && len(w.writeTimes) == 0 {
				delete(rl.windows, id)
			}
		}
		rl.mu.Unlock()
	}
}
