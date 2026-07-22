package telemetry

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const meterTelemetryCHWriteDuration = "telemetry.clickhouse.write.duration"

// Operation values for the telemetry.clickhouse.write.duration metric. Each
// names one synchronous ClickHouse write the Logger performs on a request
// path (DNO-521: these writes were invisible in traces and metrics while
// holding hook responses for seconds).
const (
	chWriteOperationInsertLogs        = "insert_telemetry_logs"
	chWriteOperationInsertLogsStaging = "insert_telemetry_logs_staging"
	chWriteOperationUpsertShadowMCP   = "upsert_shadow_mcp_inventory_urls"
)

type chWriteMetrics struct {
	writeDuration metric.Float64Histogram
}

func newCHWriteMetrics(meterProvider metric.MeterProvider, logger *slog.Logger) *chWriteMetrics {
	ctx := context.Background()
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/telemetry")

	writeDuration, err := meter.Float64Histogram(
		meterTelemetryCHWriteDuration,
		metric.WithDescription("Duration of synchronous ClickHouse telemetry writes in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10),
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to create metric", attr.SlogMetricName(meterTelemetryCHWriteDuration), attr.SlogError(err))
	}

	return &chWriteMetrics{writeDuration: writeDuration}
}

// observeCHWrite wraps a synchronous ClickHouse write in a span and duration
// metric. The span is started on the caller's context so it parents into the
// request trace, while the write itself runs on the detached shutdown context
// (it must survive request cancellation, matching the pre-existing behavior).
func (l *Logger) observeCHWrite(ctx context.Context, spanName string, operation string, rows int, write func(context.Context) error) error {
	if l.tracer == nil {
		// Stub logger: no tracing/metrics wiring, run the write as-is.
		return write(l.shutdownCtx())
	}

	start := time.Now()
	ctx, span := l.tracer.Start(ctx, spanName, trace.WithAttributes(
		attr.TelemetryCHOperation(operation),
		attr.TelemetryCHRowCount(rows),
	))
	defer span.End()

	err := write(l.shutdownCtx())
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
	}
	if l.metrics != nil && l.metrics.writeDuration != nil {
		l.metrics.writeDuration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(
			attr.TelemetryCHOperation(operation),
			attr.Outcome(o11y.OutcomeFromErrorWithTimeout(err)),
		))
	}
	return err
}
