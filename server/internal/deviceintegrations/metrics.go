package deviceintegrations

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	// meterSyncOutcome counts sync runs by provider and recorded outcome
	// (success/failure). It backs the sync failure-rate monitor. Infra errors
	// (DB unavailable) are intentionally excluded — they retry through Temporal
	// and never record a sync outcome, so they belong to Temporal's activity
	// metrics rather than this vendor-facing series.
	meterSyncOutcome = "gram.device_integration.sync.outcome"

	// meterSyncAutoPause counts schedules auto-paused after a pure streak of
	// credential rejections — the clean, rare signal an alert should fire on.
	meterSyncAutoPause = "gram.device_integration.sync.auto_pause"
)

// syncMetrics holds the sync pipeline's OpenTelemetry instruments. Instruments
// are nil-guarded at every call site, so a construction failure degrades to no
// metrics rather than a panic.
type syncMetrics struct {
	outcome   metric.Int64Counter
	autoPause metric.Int64Counter
}

func newSyncMetrics(logger *slog.Logger, meterProvider metric.MeterProvider) *syncMetrics {
	ctx := context.Background()
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/deviceintegrations")

	outcome, err := meter.Int64Counter(
		meterSyncOutcome,
		metric.WithDescription("Device integration sync runs by provider and outcome (success or failure)."),
		metric.WithUnit("{run}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterSyncOutcome), attr.SlogError(err))
	}

	autoPause, err := meter.Int64Counter(
		meterSyncAutoPause,
		metric.WithDescription("Device integration schedules auto-paused after consecutive credential rejections."),
		metric.WithUnit("{event}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterSyncAutoPause), attr.SlogError(err))
	}

	return &syncMetrics{outcome: outcome, autoPause: autoPause}
}

func (m *syncMetrics) recordOutcome(ctx context.Context, provider string, outcome o11y.Outcome) {
	if m.outcome == nil {
		return
	}
	m.outcome.Add(ctx, 1, metric.WithAttributes(
		attr.Provider(provider),
		attr.Outcome(outcome),
	))
}

func (m *syncMetrics) recordAutoPause(ctx context.Context, provider string) {
	if m.autoPause == nil {
		return
	}
	m.autoPause.Add(ctx, 1, metric.WithAttributes(
		attr.Provider(provider),
	))
}
