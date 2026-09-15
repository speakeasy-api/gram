package externalmcp

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// Metrics records parameter-header recovery without request-derived dimensions.
// Share one instance across clients. These diagnostic counters do not meter
// logical tool calls or billable usage. The zero value and nil receiver are safe.
type Metrics struct {
	headerMismatch     metric.Int64Counter
	recoveryAttempt    metric.Int64Counter
	recoverySuccess    metric.Int64Counter
	recoveryFailure    metric.Int64Counter
	recoveryExhaustion metric.Int64Counter
}

func NewMetrics(provider metric.MeterProvider, logger *slog.Logger) *Metrics {
	meter := provider.Meter("github.com/speakeasy-api/gram/server/internal/externalmcp")
	counter := func(name, description string) metric.Int64Counter {
		instrument, err := meter.Int64Counter(name, metric.WithDescription(description), metric.WithUnit("{event}"))
		if err != nil {
			logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(name), attr.SlogError(err))
		}
		return instrument
	}
	return &Metrics{
		headerMismatch:     counter("externalmcp.header.mismatches", "Parameter-header mismatch responses from upstream tool calls"),
		recoveryAttempt:    counter("externalmcp.header.recovery.attempts", "Parameter-header recovery attempts started"),
		recoverySuccess:    counter("externalmcp.header.recovery.successes", "Parameter-header recovery attempts ending in a successful tool result"),
		recoveryFailure:    counter("externalmcp.header.recovery.failures", "Parameter-header recovery attempts ending without a successful tool result"),
		recoveryExhaustion: counter("externalmcp.header.recovery.exhaustions", "Parameter-header recovery attempts whose replay also returned a mismatch"),
	}
}

// RecordHeaderMismatch counts each mismatch response, including on replay.
func (m *Metrics) RecordHeaderMismatch(ctx context.Context) {
	if m != nil && m.headerMismatch != nil {
		m.headerMismatch.Add(ctx, 1)
	}
}

// RecordRecoveryAttempt counts the start of the single permitted recovery.
func (m *Metrics) RecordRecoveryAttempt(ctx context.Context) {
	if m != nil && m.recoveryAttempt != nil {
		m.recoveryAttempt.Add(ctx, 1)
	}
}

// RecordRecoverySuccess counts a replay with neither a protocol nor tool error.
func (m *Metrics) RecordRecoverySuccess(ctx context.Context) {
	if m != nil && m.recoverySuccess != nil {
		m.recoverySuccess.Add(ctx, 1)
	}
}

// RecordRecoveryFailure counts an unsuccessful refresh or replay, including
// exhaustion. Every completed attempt records exactly one success or failure.
func (m *Metrics) RecordRecoveryFailure(ctx context.Context) {
	if m != nil && m.recoveryFailure != nil {
		m.recoveryFailure.Add(ctx, 1)
	}
}

// RecordRecoveryExhaustion counts a repeated mismatch after the one replay.
// Exhaustion is a subset of failures, not an additional recovery attempt.
func (m *Metrics) RecordRecoveryExhaustion(ctx context.Context) {
	if m != nil && m.recoveryExhaustion != nil {
		m.recoveryExhaustion.Add(ctx, 1)
	}
}
