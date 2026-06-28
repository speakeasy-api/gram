package ratelimit

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// metrics emits one counter of allow/throttle decisions, tagged by limiter name.
type metrics struct {
	decisions metric.Int64Counter
}

// WithMetrics makes the Limiter emit a decision counter tagged by limiter name
// and outcome. Callers that already record richer (e.g. per-org) metrics can
// omit it.
func WithMetrics(meterProvider metric.MeterProvider) Option {
	return func(l *Limiter) {
		meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/ratelimit")
		decisions, err := meter.Int64Counter(
			"ratelimit.decisions",
			metric.WithDescription("Rate limiter decisions, tagged by limiter name and whether the request was allowed"),
			metric.WithUnit("{decision}"),
		)
		if err != nil {
			// A static instrument name does not fail in practice; if it ever
			// does, run without metrics rather than break rate limiting.
			return
		}
		l.metrics = &metrics{decisions: decisions}
	}
}

func (m *metrics) recordDecision(ctx context.Context, name string, allowed bool) {
	m.decisions.Add(ctx, 1, metric.WithAttributes(
		attribute.String("ratelimit.name", name),
		attribute.Bool("ratelimit.allowed", allowed),
	))
}
