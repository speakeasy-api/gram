package guardian

import (
	"context"
	"log/slog"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const (
	outcomeAllowed  = "allowed"
	outcomeRejected = "rejected"
	outcomeError    = "error"
)

// partitionAttrs decomposes a partition key into the shared resilience
// telemetry attributes so metrics can be filtered and grouped per dependency
// (namespace), per upstream (partition), and per tenant (subset).
func partitionAttrs(key Partition) []attribute.KeyValue {
	return []attribute.KeyValue{
		attr.ResilienceNamespace(key.Namespace()),
		attr.ResiliencePartition(key.Partition()),
		attr.ResilienceSubset(key.Subset()),
	}
}

// partitionTracker records the distinct partitions a noop implementation has
// admitted so [registerInstanceGauge] can report the instance cardinality a
// real implementation would hold. Entries are never evicted, matching the
// real implementations, so the counts preview their memory footprint too. A
// nil tracker records nothing and reports no namespaces, which keeps the
// zero-value noops inert.
type partitionTracker struct {
	namespaces sync.Map // Partition.String() -> namespace
}

func (t *partitionTracker) observe(key Partition) {
	if t == nil {
		return
	}

	t.namespaces.LoadOrStore(key.String(), key.Namespace())
}

func (t *partitionTracker) countByNamespace() map[string]int64 {
	counts := make(map[string]int64)
	if t == nil {
		return counts
	}

	t.namespaces.Range(func(_, val any) bool {
		if namespace, ok := val.(string); ok {
			counts[namespace]++
		}

		return true
	})

	return counts
}

// registerInstanceGauge registers an asynchronous gauge that reports how many
// resilience instances (circuit breakers, rate limit buckets) are resident in
// this process, grouped by namespace. Instances are created per partition and
// never evicted, so the gauge doubles as a watchdog for unbounded partition
// cardinality leaking memory.
func registerInstanceGauge(logger *slog.Logger, meter metric.Meter, name, description, unit string, countByNamespace func() map[string]int64) {
	logger = logger.With(attr.SlogComponent("otel"))

	gauge, err := meter.Int64ObservableGauge(
		name,
		metric.WithDescription(description),
		metric.WithUnit(unit),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create resilience instance gauge", attr.SlogError(err), attr.SlogMetricName(name))
		return
	}

	_, err = meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		for namespace, count := range countByNamespace() {
			o.ObserveInt64(gauge, count, metric.WithAttributes(attr.ResilienceNamespace(namespace)))
		}

		return nil
	}, gauge)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to register resilience instance gauge callback", attr.SlogError(err), attr.SlogMetricName(name))
	}
}
