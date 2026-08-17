package otel

import (
	"context"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"go.opentelemetry.io/otel/metric"
)

type metrics struct {
	enricherDuration metric.Float64Histogram
}

func newMetrics(logger *slog.Logger, meterProvider metric.MeterProvider) *metrics {
	ctx := context.Background()
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/otel")

	enricherDuration, err := meter.Float64Histogram(
		meterSpanEnricherDuration,
		metric.WithDescription("Duration of a single span enricher in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.01, 0.05, 0.25, 1, 2, 5),
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to create metric", attr.SlogMetricName(meterSpanEnricherDuration), attr.SlogError(err))
	}

	return &metrics{
		enricherDuration: enricherDuration,
	}
}

func (m *metrics) recordEnricherDuration(ctx context.Context, enricherName string, duration float64, outcome o11y.Outcome) {
	m.enricherDuration.Record(
		ctx,
		duration,
		metric.WithAttributes(
			attr.OTELSpanEnricherName(enricherName),
			attr.Outcome(outcome),
		),
	)
}
