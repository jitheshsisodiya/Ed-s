package redis

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

const rateLimitKeyPrefix = "nexusvpn:ratelimit:"

// RateLimiter is a Redis-backed fixed-window rate limiter implementing
// domain.RateLimiter.
type RateLimiter struct {
	rdb *redis.Client
}

// NewRateLimiter builds a RateLimiter.
func NewRateLimiter(rdb *redis.Client) *RateLimiter {
	return &RateLimiter{rdb: rdb}
}

// Allow increments the counter for key and reports whether it is still
// within limit for the current window. The window starts on the first
// increment and the key expires after window, so it's a simple (not
// sliding) fixed window — adequate for slowing down brute-force attempts.
func (r *RateLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	fullKey := rateLimitKeyPrefix + key
	count, err := r.rdb.Incr(ctx, fullKey).Result()
	if err != nil {
		return false, err
	}
	if count == 1 {
		r.rdb.Expire(ctx, fullKey, window)
	}
	return count <= int64(limit), nil
}
