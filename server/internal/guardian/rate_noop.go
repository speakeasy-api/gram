package guardian

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/conv"
)

// NoopLimiter is a [Limiter] that admits everything. The zero value is the
// default on a [Policy], so rate limits configured via [WithResilience] are
// inert until a real limiter is injected with [WithLimiter]. A NoopLimiter
// built with [NewNoopLimiter] additionally reports the
// gram.rate_limit.buckets gauge, counting the buckets a real limiter would
// hold, so partition cardinality can be observed before enforcement is
// switched on.
type NoopLimiter struct {
	partitions *partitionTracker
}

var _ Limiter = NoopLimiter{partitions: nil}

// NewNoopLimiter creates a NoopLimiter that tracks the partitions it admits
// and reports them on the same bucket-count gauge as [NewInProcLimiter], one
// count per distinct partition grouped by namespace.
func NewNoopLimiter(logger *slog.Logger, meterProvider metric.MeterProvider) NoopLimiter {
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/guardian")

	partitions := new(partitionTracker)

	registerInstanceGauge(logger, meter,
		"gram.rate_limit.buckets",
		"Number of rate limit buckets resident in this process",
		"{bucket}",
		partitions.countByNamespace,
	)

	return NoopLimiter{partitions: partitions}
}

func (l NoopLimiter) AllowN(ctx context.Context, key Partition, limit Limit, n uint32) (RateLimitResult, error) {
	// On 32-bit builds a uint32 above math.MaxInt32 wraps negative when
	// narrowed to int; reject it like the real limiters do so swapping
	// implementations never changes which configs are accepted.
	allowed, allowedClamped := conv.ClampedUint32ToInt(n)
	remaining, remainingClamped := conv.ClampedUint32ToInt(limit.Burst)
	if allowedClamped || remainingClamped {
		return RateLimitResult{}, fmt.Errorf("noop rate limit: burst %d or cost %d is not representable on this platform", limit.Burst, n)
	}

	// Track after validation: the real limiters only create a bucket once a
	// request passes their config checks.
	l.partitions.observe(key)

	return RateLimitResult{
		Limit:      limit,
		Allowed:    allowed,
		Remaining:  remaining,
		RetryAfter: -1,
		ResetAfter: 0,
	}, nil
}
