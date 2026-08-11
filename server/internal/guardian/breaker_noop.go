package guardian

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/metric"
)

// NoopBreaker is a [Breaker] that admits everything and discards outcome
// reports. The zero value is the default on a [Policy], so breaker policies
// configured via [WithResilience] are inert until a real breaker is injected
// with [WithBreaker]. A NoopBreaker built with [NewNoopBreaker] additionally
// reports the gram.circuit_breaker.instances gauge, counting the breakers a
// real implementation would hold, so partition cardinality can be observed
// before enforcement is switched on.
type NoopBreaker struct {
	partitions *partitionTracker
}

var _ Breaker = NoopBreaker{partitions: nil}

// NewNoopBreaker creates a NoopBreaker that tracks the partitions it admits
// and reports them on the same instance-count gauge as [NewInProcBreaker],
// one count per distinct partition grouped by namespace.
func NewNoopBreaker(logger *slog.Logger, meterProvider metric.MeterProvider) NoopBreaker {
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/guardian")

	partitions := new(partitionTracker)

	registerInstanceGauge(logger, meter,
		"gram.circuit_breaker.instances",
		"Number of circuit breaker instances resident in this process",
		"{circuit_breaker}",
		partitions.countByNamespace,
	)

	return NoopBreaker{partitions: partitions}
}

func (b NoopBreaker) Allow(ctx context.Context, key Partition, policy BreakerPolicy) (BreakerResult, error) {
	b.partitions.observe(key)

	return BreakerResult{
		State:      BreakerStateClosed,
		Allowed:    true,
		RetryAfter: -1,
		Report:     func(bool) {},
	}, nil
}
