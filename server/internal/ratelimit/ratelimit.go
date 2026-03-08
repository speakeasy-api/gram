// Package ratelimit provides a fixed-window rate limiter backed by Redis.
package ratelimit

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/speakeasy-api/gram/server/internal/attr"
)

const (
	// windowDuration is the fixed-window duration. The counter resets at each
	// minute boundary. Up to 2x the limit can be served in bursts that straddle
	// a window boundary.
	windowDuration = 1 * time.Minute
)

// Result contains the outcome of a rate limit check.
type Result struct {
	// Allowed indicates whether the request is within the rate limit.
	Allowed bool
	// Limit is the maximum number of requests allowed in the window.
	Limit int
	// Remaining is the number of requests remaining in the current window.
	Remaining int
	// ResetAt is the time when the current window resets.
	ResetAt time.Time
}

// Limiter checks whether a request is within its rate limit.
type Limiter interface {
	Allow(ctx context.Context, key string, limit int) (Result, error)
}

// RateLimiter performs rate limit checks against Redis using a fixed-window counter.
type RateLimiter struct {
	client *redis.Client
	logger *slog.Logger
}

// New creates a new RateLimiter backed by the given Redis client.
func New(client *redis.Client, logger *slog.Logger) *RateLimiter {
	return &RateLimiter{
		client: client,
		logger: logger.With(attr.SlogComponent("ratelimit")),
	}
}

// Allow checks whether a request identified by key is within the rate limit.
// The limit parameter specifies the maximum number of requests per minute.
// It uses a fixed-window counter in Redis with automatic expiration.
func (rl *RateLimiter) Allow(ctx context.Context, key string, limit int) (Result, error) {
	now := time.Now()
	// Compute the window key based on the current minute boundary.
	windowStart := now.Truncate(windowDuration)
	windowKey := fmt.Sprintf("rl:%s:%d", key, windowStart.Unix())
	resetAt := windowStart.Add(windowDuration)

	// INCR + conditional EXPIRE in a pipeline to minimize round trips.
	pipe := rl.client.Pipeline()
	incrCmd := pipe.Incr(ctx, windowKey)
	pipe.ExpireNX(ctx, windowKey, windowDuration+10*time.Second) // TTL slightly beyond window to handle clock skew
	_, err := pipe.Exec(ctx)
	if err != nil {
		return Result{
			Allowed:   false,
			Limit:     limit,
			Remaining: 0,
			ResetAt:   resetAt,
		}, fmt.Errorf("redis pipeline exec: %w", err)
	}

	count := int(incrCmd.Val())
	remaining := max(limit-count, 0)

	return Result{
		Allowed:   count <= limit,
		Limit:     limit,
		Remaining: remaining,
		ResetAt:   resetAt,
	}, nil
}
