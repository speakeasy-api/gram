package o11y

import (
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// instrumentKindTemporalitySelector reports delta aggregation temporality for every
// instrument kind except UpDownCounters, which stay cumulative because delta is
// meaningless for a non-monotonic running value. This matches Datadog's
// recommended selector for OTLP metrics.
func instrumentKindTemporalitySelector(kind metric.InstrumentKind) metricdata.Temporality {
	switch kind {
	case metric.InstrumentKindUpDownCounter, metric.InstrumentKindObservableUpDownCounter:
		return metricdata.CumulativeTemporality
	default:
		return metricdata.DeltaTemporality
	}
}
