package aiassistant

import (
	"sync"
	"time"
)

// UserRateLimiter enforces per-user hourly and daily request limits.
type UserRateLimiter struct {
	mu    sync.Mutex
	users map[string]*userWindow
}

type userWindow struct {
	hourly []time.Time
	daily  []time.Time
}

// NewUserRateLimiter creates a new rate limiter.
func NewUserRateLimiter() *UserRateLimiter {
	return &UserRateLimiter{
		users: make(map[string]*userWindow),
	}
}

// Allow checks whether the user is within both hourly and daily limits.
// Returns true if allowed, or false with a human-readable reason.
func (rl *UserRateLimiter) Allow(username string, hourlyLimit, dailyLimit int) (bool, string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	w, ok := rl.users[username]
	if !ok {
		w = &userWindow{}
		rl.users[username] = w
	}

	// Prune old entries.
	hourAgo := now.Add(-1 * time.Hour)
	dayAgo := now.Add(-24 * time.Hour)
	w.hourly = pruneOlderThan(w.hourly, hourAgo)
	w.daily = pruneOlderThan(w.daily, dayAgo)

	// Check hourly limit.
	if hourlyLimit > 0 && len(w.hourly) >= hourlyLimit {
		return false, "hourly rate limit exceeded, try again later"
	}

	// Check daily limit.
	if dailyLimit > 0 && len(w.daily) >= dailyLimit {
		return false, "daily rate limit exceeded, try again tomorrow"
	}

	// Record this request.
	w.hourly = append(w.hourly, now)
	w.daily = append(w.daily, now)
	return true, ""
}

func pruneOlderThan(times []time.Time, cutoff time.Time) []time.Time {
	i := 0
	for _, t := range times {
		if t.After(cutoff) {
			times[i] = t
			i++
		}
	}
	return times[:i]
}
