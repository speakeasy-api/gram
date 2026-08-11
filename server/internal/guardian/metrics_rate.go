package guardian

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/metric"
	"golang.org/x/time/rate"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

type rateLimitMetrics struct {
	logger      *slog.Logger
	requests    metric.Int64Counter
	retryAfter  metric.Float64Histogram
	utilization metric.Float64Histogram
}

func newRateLimitMetrics(logger *slog.Logger, meter metric.Meter) *rateLimitMetrics {
	logger = logger.With(attr.SlogComponent("otel"))

	requests, err := meter.Int64Counter(
		"gram.rate_limit.requests",
		metric.WithDescription("Number of requests admitted or rejected by a rate limiter"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create rate limit requests counter", attr.SlogError(err))
	}

	retryAfter, err := meter.Float64Histogram(
		"gram.rate_limit.retry_after",
		metric.WithDescription("Time until a rate limited request would be permitted again"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create rate limit retry after histogram", attr.SlogError(err))
	}

	utilization, err := meter.Float64Histogram(
		"gram.rate_limit.utilization",
		metric.WithDescription("Fraction of a rate limit bucket's burst capacity in use at check time"),
		metric.WithUnit("1"),
		metric.WithExplicitBucketBoundaries(0.1, 0.25, 0.5, 0.75, 0.9, 0.95, 0.99, 1),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create rate limit utilization histogram", attr.SlogError(err))
	}

	return &rateLimitMetrics{
		logger:      logger,
		requests:    requests,
		retryAfter:  retryAfter,
		utilization: utilization,
	}
}

// recordCheck records the outcome of a rate limit admission check. An error
// counts as its own outcome — with the resilience layer it fails the request,
// so limiter infrastructure failures must be distinguishable from ordinary
// throttling.
func (m *rateLimitMetrics) recordCheck(ctx context.Context, key Partition, result RateLimitResult, err error) {
	attrs := partitionAttrs(key)

	if m.requests != nil {
		outcome := outcomeAllowed
		switch {
		case err != nil:
			outcome = outcomeError
		case result.Allowed == 0:
			outcome = outcomeRejected
		}

		m.requests.Add(ctx, 1, metric.WithAttributes(append(attrs, attr.Outcome(outcome))...))
	}

	if err != nil {
		return
	}

	// InfDuration means the request exceeds burst capacity and can never be
	// admitted; it would poison the distribution.
	if m.retryAfter != nil && result.Allowed == 0 && result.RetryAfter > 0 && result.RetryAfter < rate.InfDuration {
		m.retryAfter.Record(ctx, result.RetryAfter.Seconds(), metric.WithAttributes(attrs...))
	}

	if m.utilization != nil && result.Limit.Burst > 0 {
		used := (float64(result.Limit.Burst) - float64(result.Remaining)) / float64(result.Limit.Burst)
		m.utilization.Record(ctx, min(max(used, 0), 1), metric.WithAttributes(attrs...))
	}
}
