package hooks

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const (
	meterHooksEventsReceived = "hooks.events.received"

	hookMetricOutcomeAccepted     = "accepted"
	hookMetricOutcomeUnauthorized = "unauthorized"
)

type metrics struct {
	eventsReceived metric.Int64Counter
}

func newMetrics(meterProvider metric.MeterProvider, logger *slog.Logger) *metrics {
	ctx := context.Background()
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/hooks")

	eventsReceived, err := meter.Int64Counter(
		meterHooksEventsReceived,
		metric.WithDescription("Number of hook endpoint events received"),
		metric.WithUnit("{hook}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to create metric", attr.SlogMetricName(meterHooksEventsReceived), attr.SlogError(err))
	}

	return &metrics{
		eventsReceived: eventsReceived,
	}
}

func (m *metrics) RecordHookEventReceived(ctx context.Context, source string, eventName string, outcome string, orgSlug string) {
	if m == nil || m.eventsReceived == nil {
		return
	}

	attrs := []attribute.KeyValue{
		attr.HookSource(source),
		attr.HookEvent(eventName),
		attr.Outcome(outcome),
	}
	if orgSlug != "" {
		attrs = append(attrs, attr.OrganizationSlug(orgSlug))
	}

	m.eventsReceived.Add(ctx, 1, metric.WithAttributes(attrs...))
}
