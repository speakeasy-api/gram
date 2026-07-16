package guardian

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel/metric"
	"golang.org/x/time/rate"

	"github.com/speakeasy-api/gram/server/internal/conv"
)

type InProcLimiter struct {
	metrics *rateLimitMetrics
	buckets *sync.Map
}

var _ Limiter = (*InProcLimiter)(nil)

func NewInProcLimiter(logger *slog.Logger, meterProvider metric.MeterProvider) *InProcLimiter {
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/guardian")

	i := &InProcLimiter{
		metrics: newRateLimitMetrics(logger, meter),
		buckets: new(sync.Map),
	}

	registerInstanceGauge(logger, meter,
		"gram.rate_limit.buckets",
		"Number of rate limit buckets resident in this process",
		"{bucket}",
		func() map[string]int64 {
			counts := make(map[string]int64)
			i.buckets.Range(func(_, val any) bool {
				if entry, ok := val.(*bucketEntry); ok {
					counts[entry.namespace]++
				}

				return true
			})

			return counts
		},
	)

	return i
}

// bucketEntry pairs a partition's token bucket with its namespace so the
// bucket-count gauge can attribute growth to a dependency.
type bucketEntry struct {
	limiter   *rate.Limiter
	namespace string
}

func (i *InProcLimiter) AllowN(
	ctx context.Context,
	key Partition,
	limit Limit,
	n uint32,
) (RateLimitResult, error) {
	result, err := i.allowN(key, limit, n)
	i.metrics.recordCheck(ctx, key, result, err)

	return result, err
}

func (i *InProcLimiter) allowN(key Partition, limit Limit, n uint32) (RateLimitResult, error) {
	switch {
	case limit.Rate == 0:
		return RateLimitResult{}, fmt.Errorf("in-proc rate limit: rate must be greater than zero")
	case limit.Burst == 0:
		return RateLimitResult{}, fmt.Errorf("in-proc rate limit: burst must be greater than zero")
	case limit.Period <= 0:
		return RateLimitResult{}, fmt.Errorf("in-proc rate limit: period must be greater than zero, got %s", limit.Period)
	}

	// A cost above burst can never be satisfied, so mark the denial permanent.
	// This must precede the representability check: on 32-bit builds an
	// oversized cost would otherwise read as a limiter failure instead of a
	// denial. Mirrors the Redis limiter.
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
	// narrowed to int, which would misconfigure the limiter; reject rather
	// than clamp so behavior stays deterministic across architectures.
	burst, burstClamped := conv.ClampedUint32ToInt(limit.Burst)
	cost, costClamped := conv.ClampedUint32ToInt(n)
	if burstClamped || costClamped {
		return RateLimitResult{}, fmt.Errorf("in-proc rate limit: burst %d or cost %d is not representable on this platform", limit.Burst, n)
	}

	perSecond := rate.Limit(float64(limit.Rate) / limit.Period.Seconds())

	val, loaded := i.buckets.LoadOrStore(
		key.String(),
		&bucketEntry{limiter: rate.NewLimiter(perSecond, burst), namespace: key.Namespace()},
	)
	entry, ok := val.(*bucketEntry)
	if !ok {
		return RateLimitResult{}, fmt.Errorf("in-proc rate limit: unexpected bucket type %T", val)
	}
	limiter := entry.limiter

	now := time.Now()

	if loaded && (limiter.Limit() != perSecond || limiter.Burst() != burst) {
		limiter.SetLimitAt(now, perSecond)
		limiter.SetBurstAt(now, burst)
	}

	reservation := limiter.ReserveN(now, cost)

	allowed := cost
	retryAfter := time.Duration(-1)
	switch {
	case !reservation.OK():
		// n exceeded the bucket's burst at reservation time. The early
		// n > limit.Burst check makes this unreachable except when a
		// concurrent caller with a smaller limit shrinks the bucket between
		// the SetBurstAt above and ReserveN.
		allowed = 0
		retryAfter = rate.InfDuration
	case reservation.DelayFrom(now) > 0:
		// Tokens are not available yet and AllowN does not wait, so return
		// them and deny the request.
		retryAfter = reservation.DelayFrom(now)
		reservation.CancelAt(now)
		allowed = 0
	}

	tokens := limiter.TokensAt(now)
	remaining := max(int(tokens), 0)

	var resetAfter time.Duration
	if deficit := float64(limit.Burst) - tokens; deficit > 0 && perSecond > 0 {
		resetAfter = time.Duration(deficit / float64(perSecond) * float64(time.Second))
	}

	return RateLimitResult{
		Limit:      limit,
		Allowed:    allowed,
		Remaining:  remaining,
		RetryAfter: retryAfter,
		ResetAfter: resetAfter,
	}, nil
}
