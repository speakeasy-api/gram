package telemetry

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	telemetryv1 "github.com/speakeasy-api/gram/infra/gen/gram/telemetry/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

const (
	// pubsubOperationPublishLogs is the operation value stamped on shadow
	// publish spans.
	pubsubOperationPublishLogs = "publish_telemetry_logs_pubsub"

	// ShadowFlagDistinctID pins flag evaluation to a single constant identity
	// so percentage rollouts collapse to all-or-nothing: the shadow dual-write
	// is an infrastructure killswitch, not a per-user rollout.
	ShadowFlagDistinctID = "global"

	// publishAckAwaitTimeout bounds the detached goroutine that drains publish
	// acks for one batch. The broker publish itself is bounded separately by
	// PublishSettings.Timeout, configured where the telemetry publisher is
	// constructed.
	publishAckAwaitTimeout = 10 * time.Second
)

// NewNoopLogPublisher returns an inert LogPublisher: a noop Pub/Sub
// publisher, all flags off, and noop tracing/metrics. For the stub logger and
// for tests and processes that do not exercise the shadow dual-write.
func NewNoopLogPublisher(logger *slog.Logger) *LogPublisher {
	return NewLogPublisher(
		logger,
		tracenoop.NewTracerProvider(),
		metricnoop.NewMeterProvider(),
		gcp.NewNoopPublisher[*telemetryv1.LogRecord](),
		&feature.InMemory{},
	)
}

// LogPublisher mirrors rows written to the telemetry_logs ClickHouse table
// onto the gram-telemetry-v1-log-record Pub/Sub topic — the shadow dual-write
// preceding an eventual cutover to Pub/Sub-first ingestion. It is shared by
// the request-path Logger and the staged-telemetry promotion activity, the
// only two writers of telemetry_logs.
type LogPublisher struct {
	logger *slog.Logger
	tracer trace.Tracer
	pub    gcp.Publisher[*telemetryv1.LogRecord]
	flags  feature.Provider

	// drains tracks in-flight ack-drain goroutines so tests can await them
	// deterministically (see WaitForPublishDrains in export_test.go).
	drains sync.WaitGroup
}

// NewLogPublisher constructs a LogPublisher. Callers must always pass a
// publisher — a real Pub/Sub publisher, gcp.NewNoopPublisher where the shadow
// write is not wanted, or a mock in tests — and a feature provider. The meter
// provider is currently unused (publish metrics were pulled pending a rethink)
// but stays in the signature so reintroducing them is not a wiring change.
func NewLogPublisher(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	_ metric.MeterProvider,
	pub gcp.Publisher[*telemetryv1.LogRecord],
	flags feature.Provider,
) *LogPublisher {
	inv.Require(
		"telemetry log publisher",
		"publisher set", pub != nil,
		"feature provider set", flags != nil,
	)

	return &LogPublisher{
		logger: logger.With(attr.SlogComponent("telemetry_log_publisher")),
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/telemetry"),
		pub:    pub,
		flags:  flags,
		drains: sync.WaitGroup{},
	}
}

// PublishLogs mirrors rows just written to telemetry_logs onto the shadow
// topic. It is best-effort and non-blocking: it never blocks on broker acks
// (results are drained on a detached goroutine) and must never affect the
// ClickHouse write path. The flag check fails closed.
func (p *LogPublisher) PublishLogs(ctx context.Context, rows []repo.InsertTelemetryLogParams) {
	if len(rows) == 0 {
		return
	}

	// Callers invoke this after ClickHouse accepted the rows, so caller
	// cancellation (request teardown, activity cancellation) must not abort
	// the mirror: a row skipped here is never re-published — any retry finds
	// it already in telemetry_logs and takes the dedupe path. Detach
	// cancellation while keeping trace context; the publisher's own
	// PublishSettings.Timeout and publishAckAwaitTimeout bound the work
	// instead.
	ctx = context.WithoutCancel(ctx)

	enabled, err := p.flags.IsFlagEnabledLocal(ctx, feature.FlagTelemetryLogsPubSubShadow, ShadowFlagDistinctID, nil, nil)
	if err != nil {
		p.logger.WarnContext(ctx, "failed to evaluate telemetry pubsub shadow flag", attr.SlogError(err))
		return
	}
	if !enabled {
		return
	}

	ctx, span := p.tracer.Start(ctx, "telemetry.publishLogs", trace.WithAttributes(
		attr.TelemetryCHOperation(pubsubOperationPublishLogs),
		attr.TelemetryCHRowCount(len(rows)),
	))
	defer span.End()

	results := make([]gcp.PublishResult, len(rows))
	for i, row := range rows {
		results[i] = p.pub.Publish(ctx, logRecordFromInsertParams(row))
	}

	p.drains.Add(1)
	go p.drainPublishAcks(ctx, results)
}

// drainPublishAcks waits for every publish result of one batch and surfaces
// failures in a single error log.
func (p *LogPublisher) drainPublishAcks(ctx context.Context, results []gcp.PublishResult) {
	defer p.drains.Done()

	ctx, cancel := context.WithTimeout(ctx, publishAckAwaitTimeout)
	defer cancel()

	var firstErr error
	failed := 0
	for _, res := range results {
		if _, err := res.Get(ctx); err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	if firstErr != nil {
		p.logger.ErrorContext(ctx, "failed to publish telemetry logs to pubsub",
			attr.SlogError(firstErr),
			attr.SlogTelemetryPublishFailedCount(failed),
			attr.SlogTelemetryCHRowCount(len(results)),
		)
	}
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
