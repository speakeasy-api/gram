package ratelimit

import (
	"context"
	"fmt"

	"github.com/go-redis/redis_rate/v10"
	"github.com/redis/go-redis/v9"
)

// redisStore is the distributed Store. It delegates the atomic take to
// go-redis/redis_rate — the official GCRA limiter from the go-redis maintainers
// — so one cap holds across every replica through a single Redis, rather than
// the per-replica state of the in-memory limiters this package replaces.
type redisStore struct {
	limiter *redis_rate.Limiter
}

// NewRedisStore returns a Store whose buckets live in Redis. client is the
// shared go-redis client.
func NewRedisStore(client *redis.Client) Store {
	return &redisStore{limiter: redis_rate.NewLimiter(client)}
}

func (s *redisStore) take(ctx context.Context, key string, r Rate, n int) (Result, error) {
	res, err := s.limiter.AllowN(ctx, key, redis_rate.Limit{
		Rate:   r.Tokens,
		Burst:  r.Burst,
		Period: r.Interval,
	}, n)
	if err != nil {
		return Result{Allowed: false, Remaining: 0, RetryAfter: 0}, fmt.Errorf("redis rate limit: %w", err)
	}

	// AllowN is all-or-nothing: Allowed is n when the whole request fits, else 0.
	return Result{
		Allowed:    res.Allowed > 0,
		Remaining:  res.Remaining,
		RetryAfter: res.RetryAfter,
	}, nil
}
