package o11y

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// ⚠️ Use custom metrics judiciously, excessive tag cardinality for custom metrics (user IDs, request IDs) can become expensive
// See: https://docs.datadoghq.com/account_management/billing/custom_metrics/?tab=countrate
// Metrics should be used for high value telemetry on key events

type MetricName string

const (
	MetricNameToolCallCounter MetricName = "tool.call"
)

type MetricsHandler struct {
	meter metric.Meter
}

func NewMetricsHandler() *MetricsHandler {
	return &MetricsHandler{
		meter: otel.Meter("gram"),
	}
}

func (m *MetricsHandler) IncCounter(ctx context.Context, name MetricName, attrs ...attribute.KeyValue) error {
	counter, err := m.meter.Int64Counter(string(name))
	if err != nil {
		// The caller will decide what to do, they should typically gracefully handle this error
		return err
	}
	counter.Add(ctx, 1, metric.WithAttributes(attrs...))

	return nil
}
