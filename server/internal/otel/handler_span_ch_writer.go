package otel

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/otel/chrepo"
	"github.com/speakeasy-api/gram/server/internal/streams"
)

const (
	meterSpanCHWriterSpansSkipped  = "gram.otel_ch_writer.spans_skipped"
	meterSpanCHWriterSpansInserted = "gram.otel_ch_writer.spans_inserted"
)

// maxEventAnyValueDepth bounds the recursive OTLP AnyValue -> JSON mapping in
// the log and span CH writers. OTLP attribute values are practically shallow;
// anything nested deeper is attacker-shaped and encoded as null past the cap.
const maxEventAnyValueDepth = 32

// OTelTraceInserter writes a batch of span rows to ClickHouse. *chrepo.Queries
// satisfies it; tests supply a fake.
type OTelTraceInserter interface {
	InsertOTelTraces(ctx context.Context, rows []chrepo.OTelTraceRow) error
}

// SpanEventCHWriter consumes normalized gram.otel.v1.Span messages off the
// shared Pub/Sub topic and writes them to the ClickHouse otel_traces table
// that powers the Event Feed. Invalid messages are poison records: they are
// logged and acknowledged, while ClickHouse failures are returned so the batch
// is redelivered — otel_traces is plain MergeTree, so redelivery can produce
// duplicate rows that readers tolerate.
type SpanEventCHWriter struct {
	logger        *slog.Logger
	inserter      OTelTraceInserter
	spansSkipped  metric.Int64Counter
	spansInserted metric.Int64Counter
}

func NewSpanEventCHWriter(
	logger *slog.Logger,
	meterProvider metric.MeterProvider,
	inserter OTelTraceInserter,
) *SpanEventCHWriter {
	logger = logger.With(attr.SlogComponent("otel-span-ch-writer"))
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/otel")
	spansSkipped, err := meter.Int64Counter(
		meterSpanCHWriterSpansSkipped,
		metric.WithDescription("OTEL spans dropped by the ClickHouse event feed writer as unprocessable"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(meterSpanCHWriterSpansSkipped), attr.SlogError(err))
	}
	spansInserted, err := meter.Int64Counter(
		meterSpanCHWriterSpansInserted,
		metric.WithDescription("OTEL spans the ClickHouse event feed writer attempted to insert"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(meterSpanCHWriterSpansInserted), attr.SlogError(err))
	}

	return &SpanEventCHWriter{
		logger:        logger,
		inserter:      inserter,
		spansSkipped:  spansSkipped,
		spansInserted: spansInserted,
	}
}

var _ streams.BatchHandler[*otelv1.Span] = (*SpanEventCHWriter)(nil)

func (w *SpanEventCHWriter) HandleBatch(ctx context.Context, messages []*otelv1.Span, _ []gcp.MessageMetadata) error {
	rows := make([]chrepo.OTelTraceRow, 0, len(messages))
	for _, message := range messages {
		row, skipReason := spanEventRow(message)
		if skipReason != "" {
			w.logger.ErrorContext(ctx, "skipping unprocessable otel span",
				attr.SlogReason(skipReason),
				attr.SlogValueString(hexEventID(message.GetSpanId())),
			)
			if w.spansSkipped != nil {
				w.spansSkipped.Add(ctx, 1, metric.WithAttributes(attr.Reason(skipReason)))
			}
			continue
		}
		rows = append(rows, row)
	}

	if len(rows) == 0 {
		return nil
	}

	err := w.inserter.InsertOTelTraces(ctx, rows)
	if w.spansInserted != nil {
		w.spansInserted.Add(ctx, int64(len(rows)), metric.WithAttributes(attr.Outcome(o11y.OutcomeFromError(err))))
	}
	if err != nil {
		return fmt.Errorf("insert otel span events: %w", err)
	}
	return nil
}

// spanEventRow maps a normalized span to its otel_traces row. A non-empty
// skipReason marks the span unprocessable; redelivery cannot fix such a span,
// so the caller drops it instead of failing the batch.
func spanEventRow(span *otelv1.Span) (chrepo.OTelTraceRow, string) {
	var zero chrepo.OTelTraceRow

	if span == nil {
		return zero, "nil_span"
	}
	organizationID := span.GetProvenance().GetOrganizationId()
	if organizationID == "" {
		return zero, "missing_organization_id"
	}
	traceID := hexEventID(span.GetTraceId())
	spanID := hexEventID(span.GetSpanId())
	if traceID == "" || spanID == "" {
		return zero, "missing_span_identity"
	}

	// Span timing is validated at the ingest edge (non-zero start, end >=
	// start), so the fallbacks below only guard against records that predate
	// or bypass that validation. The start time is the table's sort,
	// partition, and TTL key, so it must never be epoch zero: a span with no
	// usable timestamp at all is dropped.
	startNano := eventUnixNano(span.GetStartTimeUnixNano())
	endNano := eventUnixNano(span.GetEndTimeUnixNano())
	if startNano == 0 {
		startNano = endNano
	}
	if startNano == 0 {
		return zero, "missing_timestamp"
	}
	durationNano := max(endNano-startNano, 0)

	spanAttributes, err := spanEventAttributesJSON(span.GetAttributes())
	if err != nil {
		return zero, "encode_span_attributes"
	}
	resourceAttributes, err := spanEventAttributesJSON(span.GetResource().GetAttributes())
	if err != nil {
		return zero, "encode_resource_attributes"
	}
	scopeAttributes, err := spanEventAttributesJSON(span.GetScope().GetAttributes())
	if err != nil {
		return zero, "encode_scope_attributes"
	}

	return chrepo.OTelTraceRow{
		OrganizationID:     organizationID,
		ProjectID:          span.GetProvenance().GetProjectId(),
		TimeUnixNano:       startNano,
		DurationNano:       durationNano,
		Source:             canonicalEventSource(spanEventServiceName(span)),
		TraceID:            traceID,
		SpanID:             spanID,
		ParentSpanID:       hexEventID(span.GetParentSpanId()),
		SpanName:           span.GetName(),
		SpanKind:           spanKindString(span.GetKind()),
		StatusCode:         spanStatusCodeString(span.GetStatus().GetCode()),
		StatusMessage:      span.GetStatus().GetMessage(),
		TraceState:         span.GetTraceState(),
		SpanAttributes:     spanAttributes,
		ResourceAttributes: resourceAttributes,
		ResourceSchemaURL:  span.GetResourceSchemaUrl(),
		ScopeName:          span.GetScope().GetName(),
		ScopeVersion:       span.GetScope().GetVersion(),
		ScopeAttributes:    scopeAttributes,
	}, ""
}

func spanEventServiceName(span *otelv1.Span) string {
	for _, kv := range span.GetResource().GetAttributes() {
		if kv.GetKey() == serviceNameAttribute && kv.GetValue().HasStringValue() {
			return kv.GetValue().GetStringValue()
		}
	}
	return ""
}

func spanKindString(kind otelv1.Span_SpanKind) string {
	switch kind {
	case otelv1.Span_SPAN_KIND_UNSPECIFIED:
		return "unspecified"
	case otelv1.Span_SPAN_KIND_INTERNAL:
		return "internal"
	case otelv1.Span_SPAN_KIND_SERVER:
		return "server"
	case otelv1.Span_SPAN_KIND_CLIENT:
		return "client"
	case otelv1.Span_SPAN_KIND_PRODUCER:
		return "producer"
	case otelv1.Span_SPAN_KIND_CONSUMER:
		return "consumer"
	default:
		return "unspecified"
	}
}

func spanStatusCodeString(code otelv1.Span_StatusCode) string {
	switch code {
	case otelv1.Span_STATUS_CODE_UNSPECIFIED:
		return "unspecified"
	case otelv1.Span_STATUS_CODE_OK:
		return "ok"
	case otelv1.Span_STATUS_CODE_ERROR:
		return "error"
	default:
		return "unspecified"
	}
}

// spanEventAttributesJSON flattens a key/value list into a stringified JSON
// object for a ClickHouse JSON column. Duplicate keys keep the last value.
// Dotted keys are stored as-is: ClickHouse unflattens them into nested paths.
func spanEventAttributesJSON(attributes []*otelv1.Span_KeyValue) (string, error) {
	if len(attributes) == 0 {
		return "{}", nil
	}

	values := make(map[string]any, len(attributes))
	for _, kv := range attributes {
		values[kv.GetKey()] = spanEventAnyValue(kv.GetValue())
	}

	encoded, err := json.Marshal(values)
	if err != nil {
		return "", fmt.Errorf("encode span attributes: %w", err)
	}
	return string(encoded), nil
}

func spanEventAnyValue(value *otelv1.Span_AnyValue) any {
	return spanEventAnyValueAtDepth(value, 0)
}

func spanEventAnyValueAtDepth(value *otelv1.Span_AnyValue, depth int) any {
	if depth >= maxEventAnyValueDepth {
		return nil
	}
	switch value.WhichValue() {
	case otelv1.Span_AnyValue_Value_not_set_case:
		return nil
	case otelv1.Span_AnyValue_StringValue_case:
		return value.GetStringValue()
	case otelv1.Span_AnyValue_BoolValue_case:
		return value.GetBoolValue()
	case otelv1.Span_AnyValue_IntValue_case:
		return value.GetIntValue()
	case otelv1.Span_AnyValue_DoubleValue_case:
		return value.GetDoubleValue()
	case otelv1.Span_AnyValue_ArrayValue_case:
		values := value.GetArrayValue().GetValues()
		result := make([]any, len(values))
		for i, item := range values {
			result[i] = spanEventAnyValueAtDepth(item, depth+1)
		}
		return result
	case otelv1.Span_AnyValue_KvlistValue_case:
		values := value.GetKvlistValue().GetValues()
		result := make(map[string]any, len(values))
		for _, item := range values {
			result[item.GetKey()] = spanEventAnyValueAtDepth(item.GetValue(), depth+1)
		}
		return result
	case otelv1.Span_AnyValue_BytesValue_case:
		return value.GetBytesValue()
	}
	return nil
}
