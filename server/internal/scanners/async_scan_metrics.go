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
