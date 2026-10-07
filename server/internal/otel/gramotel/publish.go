package gramotel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"google.golang.org/protobuf/proto"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const meterRecords = "gram.gramotel.records"

// reasonInvalid and reasonPublish say why a record did not reach the topic,
// so a refused record and a Pub/Sub failure stay apart on the counter.
const (
	reasonInvalid = "invalid"
	reasonPublish = "publish"
)

// Metrics counts what the publishing core does with every record, by signal,
// outcome and, for a failure, reason. A nil *Metrics records nothing, so
// tests and callers without a meter provider need no setup.
type Metrics struct {
	records metric.Int64Counter
}

func NewMetrics(logger *slog.Logger, meterProvider metric.MeterProvider) *Metrics {
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/otel/gramotel")
	records, err := meter.Int64Counter(
		meterRecords,
		metric.WithDescription("Records Gram accepted into the OTel pipeline, or refused or failed to publish"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(meterRecords), attr.SlogError(err))
	}
	return &Metrics{records: records}
}

func (m *Metrics) record(ctx context.Context, signal Signal, outcome o11y.Outcome, reason string, count int) {
	if m == nil || m.records == nil || count == 0 {
		return
	}
	attrs := []attribute.KeyValue{attr.OTELSignal(signal), attr.Outcome(outcome)}
	if reason != "" {
		attrs = append(attrs, attr.Reason(reason))
	}
	m.records.Add(ctx, int64(count), metric.WithAttributes(attrs...))
}

// Publish is the publishing core: it validates every item, then publishes
// them all, then waits for every result.
//
// Validation runs over the whole batch before anything is published, so an
// export with one bad record is refused whole and never half-written; the
// error wraps ErrInvalid. Publishing then issues a Publish call for every
// item before it waits on any result. That ordering is deliberate: the
// Pub/Sub client batches the messages it has queued into few RPCs, and
// waiting after each publish would turn a large export into one round trip
// per record. Only after every result has settled does Publish return, so a
// nil error means the whole batch is durable on the topic.
func Publish[M any](
	ctx context.Context,
	metrics *Metrics,
	signal Signal,
	publisher gcp.Publisher[M],
	validate func(M) error,
	items []M,
) error {
	for i, item := range items {
		if err := validate(item); err != nil {
			metrics.record(ctx, signal, o11y.OutcomeFailure, reasonInvalid, len(items))
			return fmt.Errorf("%w: item %d: %w", ErrInvalid, i, err)
		}
	}

	results := make([]gcp.PublishResult, 0, len(items))
	for _, item := range items {
		results = append(results, publisher.Publish(ctx, item))
	}

	var publishErr error
	failed := 0
	for _, result := range results {
		if _, err := result.Get(ctx); err != nil {
			publishErr = errors.Join(publishErr, err)
			failed++
		}
	}
	metrics.record(ctx, signal, o11y.OutcomeSuccess, "", len(items)-failed)
	metrics.record(ctx, signal, o11y.OutcomeFailure, reasonPublish, failed)
	if publishErr != nil {
		return fmt.Errorf("publish %s records: %w", signal, publishErr)
	}
	return nil
}

// PublishLogs is Publish for log records under the log ingest contract.
func PublishLogs(
	ctx context.Context,
	metrics *Metrics,
	publisher gcp.Publisher[*otelv1.InboundLogRecord],
	records []*otelv1.InboundLogRecord,
) error {
	return Publish(ctx, metrics, SignalLog, publisher, ValidateLogRecord, records)
}

// ValidateLogRecord enforces the ingest contract on one log record before it
// is published to the inbound pipeline topic: a record id is assigned, the
// record fits the relay export budget, and trace and span ids are empty or
// exactly OTLP-sized.
func ValidateLogRecord(record *otelv1.InboundLogRecord) error {
	if record == nil {
		return errors.New("log record is required")
	}
	if record.GetRecordId() == "" {
		return errors.New("log record ID is required")
	}
	if size := proto.Size(record); size > MaxLogRecordBytes {
		return fmt.Errorf("log record exceeds maximum size of %d bytes: got %d bytes", MaxLogRecordBytes, size)
	}
	if size := len(record.GetTraceId()); size != 0 && size != TraceIDSize {
		return fmt.Errorf("trace ID must be empty or %d bytes, got %d", TraceIDSize, size)
	}
	if size := len(record.GetSpanId()); size != 0 && size != SpanIDSize {
		return fmt.Errorf("span ID must be empty or %d bytes, got %d", SpanIDSize, size)
	}
	return nil
}
