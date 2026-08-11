package risk

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	meterFindingCHMessagesInserted = "gram.risk_findings.ch_messages_inserted"
	meterFindingCHMessagesSkipped  = "gram.risk_findings.ch_messages_skipped"
	meterFindingCHMessagesExcluded = "gram.risk_findings.ch_messages_excluded"
)

type metrics struct {
	chMessagesInserted metric.Int64Counter
	chMessagesSkipped  metric.Int64Counter
	chMessagesExcluded metric.Int64Counter
}

func newMetrics(meterProvider metric.MeterProvider, logger *slog.Logger) *metrics {
	ctx := context.Background()
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/risk")

	chMessagesInserted, err := meter.Int64Counter(
		meterFindingCHMessagesInserted,
		metric.WithDescription("Number of risk finding messages submitted to ClickHouse"),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterFindingCHMessagesInserted), attr.SlogError(err))
	}

	chMessagesSkipped, err := meter.Int64Counter(
		meterFindingCHMessagesSkipped,
		metric.WithDescription("Number of risk finding messages skipped before being submitted to ClickHouse"),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterFindingCHMessagesSkipped), attr.SlogError(err))
	}

	chMessagesExcluded, err := meter.Int64Counter(
		meterFindingCHMessagesExcluded,
		metric.WithDescription("Number of risk finding messages annotated as excluded before being submitted to ClickHouse"),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterFindingCHMessagesExcluded), attr.SlogError(err))
	}

	return &metrics{
		chMessagesInserted: chMessagesInserted,
		chMessagesSkipped:  chMessagesSkipped,
		chMessagesExcluded: chMessagesExcluded,
	}
}

// RecordFindingCHInserts records the number of finding messages submitted to
// ClickHouse in a single batch insert along with the outcome of the insert call.
func (m *metrics) RecordFindingCHInserts(ctx context.Context, count int, outcome o11y.Outcome) {
	if m.chMessagesInserted == nil {
		return
	}
	m.chMessagesInserted.Add(ctx, int64(count), metric.WithAttributes(attr.Outcome(outcome)))
}

// RecordFindingCHSkipped records a risk finding message that was dropped before
// reaching ClickHouse, tagged with the reason it was skipped.
func (m *metrics) RecordFindingCHSkipped(ctx context.Context, reason string) {
	if m.chMessagesSkipped == nil {
		return
	}
	m.chMessagesSkipped.Add(ctx, 1, metric.WithAttributes(attr.Reason(reason)))
}

// RecordFindingCHExcluded records a risk finding message that was annotated as
// excluded (excluded_at/exclusion_id set) rather than dropped before insert.
func (m *metrics) RecordFindingCHExcluded(ctx context.Context) {
	if m.chMessagesExcluded == nil {
		return
	}
	m.chMessagesExcluded.Add(ctx, 1)
}
