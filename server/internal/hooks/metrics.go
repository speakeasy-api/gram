package hooks

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const (
	meterHooksEventDuration = "hooks.event.duration"

	hookMetricOutcomeAccepted     = "accepted"
	hookMetricOutcomeFailure      = "failure"
	hookMetricOutcomeUnauthorized = "unauthorized"
)

type metrics struct {
	eventDuration metric.Float64Histogram
}

func newMetrics(meterProvider metric.MeterProvider, logger *slog.Logger) *metrics {
	ctx := context.Background()
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/hooks")

	eventDuration, err := meter.Float64Histogram(
		meterHooksEventDuration,
		metric.WithDescription("Duration of hook endpoint event processing in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10),
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to create metric", attr.SlogMetricName(meterHooksEventDuration), attr.SlogError(err))
	}

	return &metrics{
		eventDuration: eventDuration,
	}
}

func (m *metrics) RecordHookEventDuration(ctx context.Context, source string, eventName string, outcome string, orgSlug string, duration time.Duration) {
	if m == nil || m.eventDuration == nil {
		return
	}

	m.eventDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(hookEventMetricAttributes(source, eventName, outcome, orgSlug)...))
}

func hookEventMetricAttributes(source string, eventName string, outcome string, orgSlug string) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attr.HookSource(source),
		attr.HookEvent(eventName),
		attr.Outcome(outcome),
	}
	if orgSlug != "" {
		attrs = append(attrs, attr.OrganizationSlug(orgSlug))
	}
	return attrs
}
