package rules

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RateLimiterInterface is implemented by both the in-process RateLimiter and
// the Redis-backed RedisRateLimiter. The Engine holds this interface so the
// caller can swap implementations without changing evaluation logic.
//
// Allow returns true when the call is within the limit and records it.
// Returns false (deny) when the rate limit would be exceeded.
type RateLimiterInterface interface {
	Allow(key string, limit int, window time.Duration) bool
}

// RateLimiter implements sliding window rate limiting per (agent, tool).
type RateLimiter struct {
	mu      sync.Mutex
	windows map[string][]time.Time // "agent:tool" -> timestamps
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		windows: make(map[string][]time.Time),
	}
}

// Allow returns true when the call is within the limit and records it.
// Returns false (deny) when the limit would be exceeded.
// Implements RateLimiterInterface.
func (r *RateLimiter) Allow(key string, limit int, window time.Duration) bool {
	now := time.Now()
	cutoff := now.Add(-window)

	r.mu.Lock()
	defer r.mu.Unlock()

	timestamps := r.windows[key]
	pruned := timestamps[:0]
	for _, t := range timestamps {
		if t.After(cutoff) {
			pruned = append(pruned, t)
		}
	}

	exceeded := len(pruned) >= limit
	pruned = append(pruned, now)
	r.windows[key] = pruned

	return !exceeded // true = allowed, false = denied
}

// Check returns true if the rate limit is exceeded (call should be blocked).
// It also records the current call.
// Format: "N/period" where period is "second", "minute", "hour" (e.g., "200/minute").
func (r *RateLimiter) Check(agentID, tool, limit string) bool {
	maxCalls, window, err := parseRateLimit(limit)
	if err != nil {
		return false // invalid format = don't limit
	}
	key := fmt.Sprintf("%s:%s", agentID, tool)
	return !r.Allow(key, maxCalls, window)
}

func parseRateLimit(s string) (int, time.Duration, error) {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid rate limit format: %s", s)
	}

	count, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid count in rate limit: %s", s)
	}

	var window time.Duration
	switch strings.TrimSpace(parts[1]) {
	case "second", "s":
		window = time.Second
	case "minute", "m":
		window = time.Minute
	case "hour", "h":
		window = time.Hour
	default:
		return 0, 0, fmt.Errorf("invalid period in rate limit: %s", parts[1])
	}

	return count, window, nil
}
