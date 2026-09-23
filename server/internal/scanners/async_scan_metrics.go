package scanners

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const (
	AsyncScanEngineReal = "real"
	AsyncScanEngineStub = "stub"

	AsyncScanOutcomeOK           = "ok"
	AsyncScanOutcomeScanError    = "scan_error"
	AsyncScanOutcomePublishError = "publish_error"
	// AsyncScanOutcomeDisabled marks a message acked untouched because the
	// handler's engine is not configured in this deployment.
	AsyncScanOutcomeDisabled = "disabled"
	// AsyncScanOutcomeNoVerdict marks a message whose engine was asked for a
	// judgement and reached none — a throttle, timeout or provider outage. The
	// scan fails open, so the message is acked with no findings; the outcome is
	// kept apart from ok because for a source whose only engine is its stream
	// consumer (prompt injection) it counts a finding that will never be
	// produced, not a clean message. It is the signal to watch when sizing the
	// judge budget against real scan volume.
	AsyncScanOutcomeNoVerdict = "no_verdict"
	// AsyncScanOutcomeShadowPublished marks a shadow-mode message the handler
	// evaluated, metered and published like an enforcing one, with the shadow
	// marker on every finding so the findings store records them for engine
	// comparison and hides them from users. Kept distinct from ok so shadow
	// traffic stays separable on the counter.
	AsyncScanOutcomeShadowPublished = "shadow_published"

	meterAsyncScanHandlerMessages = "risk.async_scan.handler_messages"
)

type AsyncScanHandlerMetrics struct {
	handledMessages metric.Int64Counter
}

func NewAsyncScanHandlerMetrics(meterProvider metric.MeterProvider, logger *slog.Logger) *AsyncScanHandlerMetrics {
	if meterProvider == nil {
		return &AsyncScanHandlerMetrics{handledMessages: nil}
	}

	ctx := context.Background()
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/scanners")
	handledMessages, err := meter.Int64Counter(
		meterAsyncScanHandlerMessages,
		metric.WithDescription("Total async risk scan messages handled by scanner, engine, gate decision, and outcome"),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterAsyncScanHandlerMessages), attr.SlogError(err))
	}

	return &AsyncScanHandlerMetrics{handledMessages: handledMessages}
}

func (m *AsyncScanHandlerMetrics) RecordHandled(ctx context.Context, orgID, scanner, engine, outcome string, gateReason AsyncShadowGateReason) {
	if m == nil || m.handledMessages == nil {
		return
	}

	m.handledMessages.Add(ctx, 1, metric.WithAttributes(
		attr.OrganizationID(orgID),
		attribute.String("scanner", scanner),
		attribute.String("engine", engine),
		attr.Outcome(outcome),
		attribute.String("gate_reason", string(gateReason)),
	))
}
