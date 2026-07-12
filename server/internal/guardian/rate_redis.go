package guardian

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/go-redis/redis_rate/v10"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/time/rate"

	"github.com/speakeasy-api/gram/server/internal/conv"
)

// RedisRateLimiter is a [Limiter] whose buckets live in Redis, so limits are
// enforced across replicas rather than per process. Use it via
// [WithLimiter] when a Policy's rate limits must hold fleet-wide.
type RedisRateLimiter struct {
	metrics *rateLimitMetrics
	source  *redis_rate.Limiter
}

var _ Limiter = (*RedisRateLimiter)(nil)

func NewRedisRateLimiter(logger *slog.Logger, meterProvider metric.MeterProvider, client redis.UniversalClient) *RedisRateLimiter {
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/guardian")

	return &RedisRateLimiter{
		metrics: newRateLimitMetrics(logger, meter),
		source:  redis_rate.NewLimiter(client),
	}
}

func (r *RedisRateLimiter) AllowN(
	ctx context.Context,
	key Partition,
	limit Limit,
	n uint32,
) (RateLimitResult, error) {
	res, err := r.allowN(ctx, key, limit, n)
	r.metrics.recordCheck(ctx, key, res, err)

	return res, err
}

func (r *RedisRateLimiter) allowN(ctx context.Context, key Partition, limit Limit, n uint32) (RateLimitResult, error) {
	var zero RateLimitResult

	// GCRA derives its emission interval as period/rate, so a zero period or
	// rate would fail open (or divide by zero) inside the Redis script.
	switch {
	case limit.Rate == 0:
		return zero, fmt.Errorf("redis rate limit: rate must be greater than zero")
	case limit.Burst == 0:
		return zero, fmt.Errorf("redis rate limit: burst must be greater than zero")
	case limit.Period <= 0:
		return zero, fmt.Errorf("redis rate limit: period must be greater than zero, got %s", limit.Period)
	}

	// A cost above burst can never be satisfied, but GCRA reports a finite
	// retry-after for it, which would send Wait-and-retry callers into a
	// futile loop. Mirror the in-proc limiter and mark the denial permanent.
	if n > limit.Burst {
		return RateLimitResult{
			Limit:      limit,
			Allowed:    0,
			Remaining:  0,
			RetryAfter: rate.InfDuration,
			ResetAfter: 0,
		}, nil
	}

	// On 32-bit builds a uint32 above math.MaxInt32 wraps negative when
	// narrowed to int, handing GCRA a negative rate or burst; reject rather
	// than clamp so behavior stays deterministic across architectures.
	rateInt, rateClamped := conv.ClampedUint32ToInt(limit.Rate)
	burstInt, burstClamped := conv.ClampedUint32ToInt(limit.Burst)
	costInt, costClamped := conv.ClampedUint32ToInt(n)
	if rateClamped || burstClamped || costClamped {
		return zero, fmt.Errorf("redis rate limit: rate %d, burst %d, or cost %d is not representable on this platform", limit.Rate, limit.Burst, n)
	}

	rres, err := r.source.AllowN(ctx, key.String(), redis_rate.Limit{
		Rate:   rateInt,
		Burst:  burstInt,
		Period: limit.Period,
	}, costInt)
	if err != nil {
		return zero, fmt.Errorf("redis rate allow-n: %w", err)
	}

	return RateLimitResult{
		Limit:      limit,
		Allowed:    rres.Allowed,
		Remaining:  rres.Remaining,
		RetryAfter: rres.RetryAfter,
		ResetAfter: rres.ResetAfter,
	}, nil
}
