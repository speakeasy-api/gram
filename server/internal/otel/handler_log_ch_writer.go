package otel

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/otel/chrepo"
	"github.com/speakeasy-api/gram/server/internal/streams"
)

const (
	meterLogCHWriterRecordsSkipped  = "gram.otel_ch_writer.log_records_skipped"
	meterLogCHWriterRecordsInserted = "gram.otel_ch_writer.log_records_inserted"
)

// OTelLogInserter writes a batch of log rows to ClickHouse. *chrepo.Queries
// satisfies it; tests supply a fake.
type OTelLogInserter interface {
	InsertOTelLogs(ctx context.Context, rows []chrepo.OTelLogRow) error
}

// LogEventCHWriter consumes normalized gram.otel.v1.LogRecord messages off the
// shared Pub/Sub topic and writes them to the ClickHouse otel_logs table that
// powers the Event Feed. Invalid messages are poison records: they are logged
// and acknowledged, while ClickHouse failures are returned so the batch is
// redelivered — safe because otel_logs dedups redelivered rows on record_id
// via ReplacingMergeTree.
type LogEventCHWriter struct {
	logger          *slog.Logger
	inserter        OTelLogInserter
	recordsSkipped  metric.Int64Counter
	recordsInserted metric.Int64Counter
}

func NewLogEventCHWriter(
	logger *slog.Logger,
	meterProvider metric.MeterProvider,
	inserter OTelLogInserter,
) *LogEventCHWriter {
	logger = logger.With(attr.SlogComponent("otel-log-ch-writer"))
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/otel")
	recordsSkipped, err := meter.Int64Counter(
		meterLogCHWriterRecordsSkipped,
		metric.WithDescription("OTEL log records dropped by the ClickHouse event feed writer as unprocessable"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(meterLogCHWriterRecordsSkipped), attr.SlogError(err))
	}
	recordsInserted, err := meter.Int64Counter(
		meterLogCHWriterRecordsInserted,
		metric.WithDescription("OTEL log records the ClickHouse event feed writer attempted to insert"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(meterLogCHWriterRecordsInserted), attr.SlogError(err))
	}

	return &LogEventCHWriter{
		logger:          logger,
		inserter:        inserter,
		recordsSkipped:  recordsSkipped,
		recordsInserted: recordsInserted,
	}
}

var _ streams.BatchHandler[*otelv1.LogRecord] = (*LogEventCHWriter)(nil)

func (w *LogEventCHWriter) HandleBatch(ctx context.Context, messages []*otelv1.LogRecord, _ []gcp.MessageMetadata) error {
	rows := make([]chrepo.OTelLogRow, 0, len(messages))
	for _, message := range messages {
		row, skipReason := logEventRow(message)
		if skipReason != "" {
			w.logger.ErrorContext(ctx, "skipping unprocessable otel log record",
				attr.SlogReason(skipReason),
				attr.SlogValueString(message.GetRecordId()),
			)
			if w.recordsSkipped != nil {
				w.recordsSkipped.Add(ctx, 1, metric.WithAttributes(attr.Reason(skipReason)))
			}
			continue
		}
		rows = append(rows, row)
	}

	if len(rows) == 0 {
		return nil
	}

	err := w.inserter.InsertOTelLogs(ctx, rows)
	if w.recordsInserted != nil {
		w.recordsInserted.Add(ctx, int64(len(rows)), metric.WithAttributes(attr.Outcome(o11y.OutcomeFromError(err))))
	}
	if err != nil {
		return fmt.Errorf("insert otel log events: %w", err)
	}
	return nil
}

// logEventRow maps a normalized log record to its otel_logs row. A non-empty
// skipReason marks the record unprocessable; redelivery cannot fix such a
// record, so the caller drops it instead of failing the batch.
func logEventRow(record *otelv1.LogRecord) (chrepo.OTelLogRow, string) {
	var zero chrepo.OTelLogRow

	if record == nil {
		return zero, "nil_record"
	}
	recordID := record.GetRecordId()
	if recordID == "" {
		return zero, "missing_record_id"
	}
	organizationID := record.GetProvenance().GetOrganizationId()
	if organizationID == "" {
		return zero, "missing_organization_id"
	}

	// The producer's event time, standing in observed time when the producer
	// did not know it, then write time. The value is the table's sort,
	// partition, and TTL key, so an epoch-zero value would park the row in a
	// 1970 partition that TTL removes on the next merge.
	observedNano := eventUnixNano(record.GetObservedTimeUnixNano())
	timeNano := eventUnixNano(record.GetTimeUnixNano())
	if timeNano == 0 {
		timeNano = observedNano
	}
	if timeNano == 0 {
		timeNano = time.Now().UnixNano()
	}

	body, err := logEventBody(record.GetBody())
	if err != nil {
		return zero, "encode_body"
	}
	logAttributes, err := logEventAttributesJSON(record.GetAttributes())
	if err != nil {
		return zero, "encode_log_attributes"
	}
	resourceAttributes, err := logEventAttributesJSON(record.GetResource().GetAttributes())
	if err != nil {
		return zero, "encode_resource_attributes"
	}
	scopeAttributes, err := logEventAttributesJSON(record.GetScope().GetAttributes())
	if err != nil {
		return zero, "encode_scope_attributes"
	}

	return chrepo.OTelLogRow{
		RecordID:             recordID,
		OrganizationID:       organizationID,
		ProjectID:            record.GetProvenance().GetProjectId(),
		TimeUnixNano:         timeNano,
		ObservedTimeUnixNano: observedNano,
		Source:               canonicalEventSource(logEventServiceName(record)),
		TraceID:              hexEventID(record.GetTraceId()),
		SpanID:               hexEventID(record.GetSpanId()),
		EventName:            record.GetEventName(),
		SeverityText:         record.GetSeverityText(),
		SeverityNumber:       int32(record.GetSeverityNumber()),
		Body:                 body,
		LogAttributes:        logAttributes,
		Flags:                record.GetFlags(),
		ResourceAttributes:   resourceAttributes,
		ResourceSchemaURL:    record.GetResourceSchemaUrl(),
		ScopeName:            record.GetScope().GetName(),
		ScopeVersion:         record.GetScope().GetVersion(),
		ScopeAttributes:      scopeAttributes,
	}, ""
}

func logEventServiceName(record *otelv1.LogRecord) string {
	for _, kv := range record.GetResource().GetAttributes() {
		if kv.GetKey() == serviceNameAttribute && kv.GetValue().HasStringValue() {
			return kv.GetValue().GetStringValue()
		}
	}
	return ""
}

// logEventBody renders the log body for the otel_logs body column: string
// bodies verbatim, unset bodies empty, structured bodies JSON-encoded.
func logEventBody(value *otelv1.LogRecord_AnyValue) (string, error) {
	if value == nil || value.WhichValue() == otelv1.LogRecord_AnyValue_Value_not_set_case {
		return "", nil
	}
	if value.HasStringValue() {
		return value.GetStringValue(), nil
	}
	encoded, err := json.Marshal(logEventAnyValue(value))
	if err != nil {
		return "", fmt.Errorf("encode structured log body: %w", err)
	}
	return string(encoded), nil
}

// logEventAttributesJSON flattens a key/value list into a stringified JSON
// object for a ClickHouse JSON column. Duplicate keys keep the last value.
// Dotted keys are stored as-is: ClickHouse unflattens them into nested paths.
func logEventAttributesJSON(attributes []*otelv1.LogRecord_KeyValue) (string, error) {
	if len(attributes) == 0 {
		return "{}", nil
	}

	values := make(map[string]any, len(attributes))
	for _, kv := range attributes {
		values[kv.GetKey()] = logEventAnyValue(kv.GetValue())
	}

	encoded, err := json.Marshal(values)
	if err != nil {
		return "", fmt.Errorf("encode log attributes: %w", err)
	}
	return string(encoded), nil
}

func logEventAnyValue(value *otelv1.LogRecord_AnyValue) any {
	switch value.WhichValue() {
	case otelv1.LogRecord_AnyValue_Value_not_set_case:
		return nil
	case otelv1.LogRecord_AnyValue_StringValue_case:
		return value.GetStringValue()
	case otelv1.LogRecord_AnyValue_BoolValue_case:
		return value.GetBoolValue()
	case otelv1.LogRecord_AnyValue_IntValue_case:
		return value.GetIntValue()
	case otelv1.LogRecord_AnyValue_DoubleValue_case:
		return value.GetDoubleValue()
	case otelv1.LogRecord_AnyValue_ArrayValue_case:
		values := value.GetArrayValue().GetValues()
		result := make([]any, len(values))
		for i, item := range values {
			result[i] = logEventAnyValue(item)
		}
		return result
	case otelv1.LogRecord_AnyValue_KvlistValue_case:
		values := value.GetKvlistValue().GetValues()
		result := make(map[string]any, len(values))
		for _, item := range values {
			result[item.GetKey()] = logEventAnyValue(item.GetValue())
		}
		return result
	case otelv1.LogRecord_AnyValue_BytesValue_case:
		return value.GetBytesValue()
	}
	return nil
}
