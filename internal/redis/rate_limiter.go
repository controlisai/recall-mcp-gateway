package redis

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const rateLimitPrefix = "ratelimit:"

// RedisRateLimiter implements sliding window rate limiting using Redis sorted sets.
// Drop-in replacement for rules.RateLimiter in multi-pod deployments.
//
// The sorted set stores call timestamps (Unix ms) as both score and member.
// Old entries outside the window are pruned on each check. Redis TTL auto-cleans idle keys.
type RedisRateLimiter struct {
	rdb *goredis.Client
}

// NewRedisRateLimiter creates a Redis-backed sliding window rate limiter.
func NewRedisRateLimiter(rdb *goredis.Client) *RedisRateLimiter {
	return &RedisRateLimiter{rdb: rdb}
}

// Allow returns true if the call is within the rate limit and records it.
// Returns false (deny) if the limit would be exceeded.
// On Redis error, fails open (allow) to avoid false blocking.
func (r *RedisRateLimiter) Allow(key string, limit int, window time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	now := time.Now()
	nowMs := now.UnixMilli()
	windowMs := now.Add(-window).UnixMilli()
	redisKey := rateLimitPrefix + key
	windowSec := int64(window.Seconds()) + 1 // +1 for rounding safety

	windowMinStr := fmt.Sprintf("%d", windowMs)

	pipe := r.rdb.Pipeline()
	// Add current call
	pipe.ZAdd(ctx, redisKey, goredis.Z{Score: float64(nowMs), Member: nowMs})
	// Remove entries outside the window
	pipe.ZRemRangeByScore(ctx, redisKey, "-inf", windowMinStr)
	// Count remaining entries
	cardCmd := pipe.ZCard(ctx, redisKey)
	// Reset TTL on each access
	pipe.Expire(ctx, redisKey, time.Duration(windowSec)*time.Second)

	if _, err := pipe.Exec(ctx); err != nil {
		slog.Warn("redis rate limiter: pipeline failed", "key", key, "error", err)
		return true // fail open
	}

	count := cardCmd.Val()
	// count already includes the entry we just added; deny if it exceeds limit.
	return count <= int64(limit)
}
