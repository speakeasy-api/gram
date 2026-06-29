package risk

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const meterFindingRowsInserted = "gram.risk_findings.bq_rows_inserted"

type metrics struct {
	rowsInserted metric.Int64Counter
}

func newMetrics(meterProvider metric.MeterProvider, logger *slog.Logger) *metrics {
	ctx := context.Background()
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/risk")

	rowsInserted, err := meter.Int64Counter(
		meterFindingRowsInserted,
		metric.WithDescription("Number of risk finding rows submitted to BigQuery"),
		metric.WithUnit("{row}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterFindingRowsInserted), attr.SlogError(err))
	}

	return &metrics{
		rowsInserted: rowsInserted,
	}
}

// RecordFindingBQInserts records the number of finding rows submitted to
// BigQuery in a single batch insert along with the outcome of the insert call.
func (m *metrics) RecordFindingBQInserts(ctx context.Context, count int, outcome o11y.Outcome) {
	if m.rowsInserted == nil {
		return
	}
	m.rowsInserted.Add(ctx, int64(count), metric.WithAttributes(attr.Outcome(outcome)))
}
