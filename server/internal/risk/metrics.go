package risk

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	meterFindingMessagesInserted = "gram.risk_findings.bq_messages_inserted"
	meterFindingMessagesSkipped  = "gram.risk_findings.bq_messages_skipped"
)

type metrics struct {
	messagesInserted metric.Int64Counter
	messagesSkipped  metric.Int64Counter
}

func newMetrics(meterProvider metric.MeterProvider, logger *slog.Logger) *metrics {
	ctx := context.Background()
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/risk")

	messagesInserted, err := meter.Int64Counter(
		meterFindingMessagesInserted,
		metric.WithDescription("Number of risk finding messages submitted to BigQuery"),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterFindingMessagesInserted), attr.SlogError(err))
	}

	messagesSkipped, err := meter.Int64Counter(
		meterFindingMessagesSkipped,
		metric.WithDescription("Number of risk finding messages skipped before being submitted to BigQuery"),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterFindingMessagesSkipped), attr.SlogError(err))
	}

	return &metrics{
		messagesInserted: messagesInserted,
		messagesSkipped:  messagesSkipped,
	}
}

// RecordFindingBQInserts records the number of finding messages submitted to
// BigQuery in a single batch insert along with the outcome of the insert call.
func (m *metrics) RecordFindingBQInserts(ctx context.Context, count int, outcome o11y.Outcome) {
	if m.messagesInserted == nil {
		return
	}
	m.messagesInserted.Add(ctx, int64(count), metric.WithAttributes(attr.Outcome(outcome)))
}

// RecordFindingSkipped records a risk finding message that was dropped before
// reaching BigQuery, tagged with the reason it was skipped.
func (m *metrics) RecordFindingSkipped(ctx context.Context, reason string) {
	if m.messagesSkipped == nil {
		return
	}
	m.messagesSkipped.Add(ctx, 1, metric.WithAttributes(attr.Reason(reason)))
}
