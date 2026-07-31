package telemetry

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	telemetryv1 "github.com/speakeasy-api/gram/infra/gen/gram/telemetry/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/pubsub"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// pubsubOperationPublishLogs is the operation value stamped on shadow publish
// spans.
const pubsubOperationPublishLogs = "publish_telemetry_logs_pubsub"

// NewNoopLogPublisher returns an inert LogPublisher: a noop Pub/Sub
// publisher and noop tracing/metrics. For the stub logger and for tests and
// processes that do not exercise the shadow dual-write.
func NewNoopLogPublisher(logger *slog.Logger) *LogPublisher {
	return NewLogPublisher(
		logger,
		tracenoop.NewTracerProvider(),
		metricnoop.NewMeterProvider(),
		gcp.NewNoopPublisher[*telemetryv1.LogRecord](),
	)
}

// LogPublisher mirrors rows written to the telemetry_logs ClickHouse table
// onto the gram-telemetry-v1-log-record Pub/Sub topic — the shadow dual-write
// preceding an eventual cutover to Pub/Sub-first ingestion. It is shared by
// the request-path Logger and the staged-telemetry promotion activity, the
// only two writers of telemetry_logs.
type LogPublisher struct {
	tracer  trace.Tracer
	pub     gcp.Publisher[*telemetryv1.LogRecord]
	drainer *pubsub.Drainer
}

// NewLogPublisher constructs a LogPublisher. Callers must always pass a
// publisher — a real Pub/Sub publisher, gcp.NewNoopPublisher where the shadow
// write is not wanted, or a mock in tests.
func NewLogPublisher(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	pub gcp.Publisher[*telemetryv1.LogRecord],
) *LogPublisher {
	inv.Require(
		"telemetry log publisher",
		"publisher set", pub != nil,
	)

	return &LogPublisher{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/telemetry"),
		pub:    pub,
		// The drainer stamps the component attribute on its own logger, so it
		// takes the raw one.
		drainer: pubsub.NewDrainer(logger, meterProvider, "telemetry_log_publisher"),
	}
}

// PublishLogs mirrors rows just written to telemetry_logs onto the shadow
// topic. It is best-effort and non-blocking: it never waits on broker acks
// (results are handed to a bounded drain pool) and must never affect the
// ClickHouse write path.
func (p *LogPublisher) PublishLogs(ctx context.Context, rows []repo.InsertTelemetryLogParams) {
	if len(rows) == 0 {
		return
	}

	// Callers invoke this after ClickHouse accepted the rows, so caller
	// cancellation (request teardown, activity cancellation) must not abort
	// the mirror: a row skipped here is never re-published — any retry finds
	// it already in telemetry_logs and takes the dedupe path. Detach
	// cancellation while keeping trace context; the publisher's own
	// PublishSettings.Timeout bounds the work instead.
	ctx = context.WithoutCancel(ctx)

	ctx, span := p.tracer.Start(ctx, "telemetry.publishLogs", trace.WithAttributes(
		attr.TelemetryCHOperation(pubsubOperationPublishLogs),
		attr.TelemetryCHRowCount(len(rows)),
	))
	defer span.End()

	results := make([]gcp.PublishResult, len(rows))
	for i, row := range rows {
		results[i] = p.pub.Publish(ctx, logRecordFromInsertParams(row))
	}

	// The span covers the enqueue only. The acks land after it closes, and are
	// reported on the pubsub.publish.ack_* counters instead.
	p.drainer.Enqueue(ctx, results...)
}

// Close releases the ack drain pool, waiting for queued publishes to resolve
// until ctx expires.
func (p *LogPublisher) Close(ctx context.Context) error {
	if err := p.drainer.Close(ctx); err != nil {
		return fmt.Errorf("close telemetry log publisher: %w", err)
	}

	return nil
}

// logRecordFromInsertParams converts one telemetry_logs row into its Pub/Sub
// representation. Nullable columns (*string fields) pass through as-is so SQL
// NULL round-trips to an unset proto field.
func logRecordFromInsertParams(row repo.InsertTelemetryLogParams) *telemetryv1.LogRecord {
	return telemetryv1.LogRecord_builder{
		Id:                     &row.ID,
		TimeUnixNano:           &row.TimeUnixNano,
		ObservedTimeUnixNano:   &row.ObservedTimeUnixNano,
		SeverityText:           row.SeverityText,
		Body:                   &row.Body,
		TraceId:                row.TraceID,
		SpanId:                 row.SpanID,
		AttributesJson:         &row.Attributes,
		ResourceAttributesJson: &row.ResourceAttributes,
		GramProjectId:          &row.GramProjectID,
		GramDeploymentId:       row.GramDeploymentID,
		GramFunctionId:         row.GramFunctionID,
		GramUrn:                &row.GramURN,
		ServiceName:            &row.ServiceName,
		ServiceVersion:         row.ServiceVersion,
		GramChatId:             row.GramChatID,
	}.Build()
}
