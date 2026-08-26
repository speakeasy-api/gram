package telemetry

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

const (
	// pubsubOperationPublishLogs is the operation value stamped on canonical
	// OTEL publish spans.
	pubsubOperationPublishLogs = "publish_otel_logs_pubsub"

	// publishAckAwaitTimeout bounds the detached goroutine that drains publish
	// acks for one batch. The broker publish itself is bounded separately by
	// PublishSettings.Timeout, configured where the telemetry publisher is
	// constructed.
	publishAckAwaitTimeout = 10 * time.Second
)

// NewNoopLogPublisher returns an inert LogPublisher: a noop Pub/Sub
// publisher and noop tracing/metrics. For the stub logger and for tests and
// processes that do not exercise the shadow dual-write.
func NewNoopLogPublisher(logger *slog.Logger) *LogPublisher {
	return NewLogPublisher(
		logger,
		tracenoop.NewTracerProvider(),
		metricnoop.NewMeterProvider(),
		gcp.NewNoopPublisher[*otelv1.InboundLogRecord](),
	)
}

// LogPublisher mirrors rows written to telemetry_logs onto the canonical OTEL
// ingest topic. The normal transform and ClickHouse writer pipeline then stores
// them in otel_logs alongside native OTLP records. It is shared by the
// request-path Logger and the staged-telemetry promotion activity.
type LogPublisher struct {
	logger *slog.Logger
	tracer trace.Tracer
	pub    gcp.Publisher[*otelv1.InboundLogRecord]

	// drains tracks in-flight ack-drain goroutines so tests can await them
	// deterministically (see WaitForPublishDrains in export_test.go).
	drains sync.WaitGroup
}

// NewLogPublisher constructs a LogPublisher. Callers must always pass a
// publisher — a real Pub/Sub publisher, gcp.NewNoopPublisher where the shadow
// write is not wanted, or a mock in tests. The meter provider is currently
// unused (publish metrics were pulled pending a rethink) but stays in the
// signature so reintroducing them is not a wiring change.
func NewLogPublisher(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	_ metric.MeterProvider,
	pub gcp.Publisher[*otelv1.InboundLogRecord],
) *LogPublisher {
	inv.Require(
		"telemetry log publisher",
		"publisher set", pub != nil,
	)

	return &LogPublisher{
		logger: logger.With(attr.SlogComponent("telemetry_log_publisher")),
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/telemetry"),
		pub:    pub,
		drains: sync.WaitGroup{},
	}
}

// PublishLogs mirrors rows just written to telemetry_logs onto the canonical
// OTEL ingest topic. It is best-effort and non-blocking: it never blocks on
// broker acks (results are drained on a detached goroutine) and must never
// affect the ClickHouse write path.
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

	ctx, span := p.tracer.Start(ctx, "telemetry.publishLogs", trace.WithAttributes(
		attr.TelemetryCHOperation(pubsubOperationPublishLogs),
		attr.TelemetryCHRowCount(len(rows)),
	))
	defer span.End()

	results := make([]gcp.PublishResult, 0, len(rows))
	for _, row := range rows {
		record, err := otelLogRecordFromInsertParams(row)
		if err != nil {
			p.logger.ErrorContext(ctx, "failed to convert telemetry log to canonical otel record",
				attr.SlogError(err),
				attr.SlogValueString(row.ID),
			)
			continue
		}
		results = append(results, p.pub.Publish(ctx, record))
	}
	if len(results) == 0 {
		return
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
		p.logger.ErrorContext(ctx, "failed to publish telemetry logs to canonical otel topic",
			attr.SlogError(firstErr),
			attr.SlogTelemetryPublishFailedCount(failed),
			attr.SlogTelemetryCHRowCount(len(results)),
		)
	}
}

// otelLogRecordFromInsertParams converts a telemetry_logs row into the
// canonical inbound OTEL shape. Dotted attribute keys remain dotted; the OTEL
// ClickHouse writer stores them in log_attributes, where ClickHouse exposes
// their canonical nested paths.
func otelLogRecordFromInsertParams(row repo.InsertTelemetryLogParams) (*otelv1.InboundLogRecord, error) {
	attributes, attributeValues, err := otelAttributesFromJSON(row.Attributes)
	if err != nil {
		return nil, fmt.Errorf("decode log attributes: %w", err)
	}
	resourceAttributes, _, err := otelAttributesFromJSON(row.ResourceAttributes)
	if err != nil {
		return nil, fmt.Errorf("decode resource attributes: %w", err)
	}

	organizationID, _ := attributeValues[string(attr.OrganizationIDKey)].(string)
	if organizationID == "" {
		return nil, fmt.Errorf("missing %s attribute", attr.OrganizationIDKey)
	}

	traceID, err := decodeOTELHexID(row.TraceID, 16)
	if err != nil {
		return nil, fmt.Errorf("decode trace id: %w", err)
	}
	spanID, err := decodeOTELHexID(row.SpanID, 8)
	if err != nil {
		return nil, fmt.Errorf("decode span id: %w", err)
	}

	eventName := stringOTELAttribute(attributeValues, string(attr.HookEventKey))
	if eventName == "" {
		eventName = eventNameFromURN(stringOTELAttribute(attributeValues, string(attr.EventURNKey)))
	}
	timeUnixNano := uint64(max(row.TimeUnixNano, 0))
	observedTimeUnixNano := uint64(max(row.ObservedTimeUnixNano, 0))
	severityNumber := otelSeverityNumber(row.SeverityText)
	body := (&otelv1.InboundLogRecord_AnyValue_builder{StringValue: &row.Body}).Build()
	source := row.ServiceName

	return otelv1.InboundLogRecord_builder{
		TimeUnixNano:           &timeUnixNano,
		SeverityNumber:         &severityNumber,
		SeverityText:           row.SeverityText,
		Body:                   body,
		Attributes:             attributes,
		DroppedAttributesCount: nil,
		Flags:                  nil,
		TraceId:                traceID,
		SpanId:                 spanID,
		ObservedTimeUnixNano:   &observedTimeUnixNano,
		EventName:              &eventName,
		RecordId:               &row.ID,
		Resource: (&otelv1.InboundLogRecord_Resource_builder{
			Attributes: resourceAttributes,
		}).Build(),
		ResourceSchemaUrl: nil,
		Scope:             nil,
		ScopeSchemaUrl:    nil,
		Provenance: (&otelv1.InboundLogRecord_Provenance_builder{
			Source:         &source,
			OrganizationId: &organizationID,
			ProjectId:      &row.GramProjectID,
		}).Build(),
	}.Build(), nil
}

func otelAttributesFromJSON(raw string) ([]*otelv1.InboundLogRecord_KeyValue, map[string]any, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()

	values := map[string]any{}
	if err := decoder.Decode(&values); err != nil {
		return nil, nil, fmt.Errorf("decode JSON object: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, nil, err
	}
	values = flattenOTELAttributes(values)

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	attributes := make([]*otelv1.InboundLogRecord_KeyValue, 0, len(keys))
	for _, key := range keys {
		value, err := otelAnyValue(values[key])
		if err != nil {
			return nil, nil, fmt.Errorf("convert attribute %q: %w", key, err)
		}
		attributes = append(attributes, (&otelv1.InboundLogRecord_KeyValue_builder{
			Key:   &key,
			Value: value,
		}).Build())
	}
	return attributes, values, nil
}

// flattenOTELAttributes restores the flat key/value representation used by
// OTLP after a row has round-tripped through a ClickHouse JSON column. The
// column auto-unflattens dotted keys (gram.org.id becomes nested JSON), while
// canonical OTEL attributes remain a flat list with dotted key names.
func flattenOTELAttributes(values map[string]any) map[string]any {
	flattened := make(map[string]any)
	var flatten func(string, any)
	flatten = func(prefix string, value any) {
		object, ok := value.(map[string]any)
		if !ok {
			flattened[prefix] = value
			return
		}
		if len(object) == 0 {
			flattened[prefix] = object
			return
		}
		for key, child := range object {
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			flatten(path, child)
		}
	}
	for key, value := range values {
		flatten(key, value)
	}
	return flattened
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	err := decoder.Decode(&trailing)
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return fmt.Errorf("decode trailing JSON value: %w", err)
	}
	return fmt.Errorf("unexpected trailing JSON value")
}

func otelAnyValue(value any) (*otelv1.InboundLogRecord_AnyValue, error) {
	switch value := value.(type) {
	case nil:
		return (&otelv1.InboundLogRecord_AnyValue_builder{}).Build(), nil
	case bool:
		return (&otelv1.InboundLogRecord_AnyValue_builder{BoolValue: &value}).Build(), nil
	case string:
		return (&otelv1.InboundLogRecord_AnyValue_builder{StringValue: &value}).Build(), nil
	case json.Number:
		if integer, err := strconv.ParseInt(string(value), 10, 64); err == nil {
			return (&otelv1.InboundLogRecord_AnyValue_builder{IntValue: &integer}).Build(), nil
		}
		floating, err := strconv.ParseFloat(string(value), 64)
		if err != nil {
			return nil, fmt.Errorf("parse JSON number: %w", err)
		}
		return (&otelv1.InboundLogRecord_AnyValue_builder{DoubleValue: &floating}).Build(), nil
	case []any:
		items := make([]*otelv1.InboundLogRecord_AnyValue, len(value))
		for i, item := range value {
			converted, err := otelAnyValue(item)
			if err != nil {
				return nil, fmt.Errorf("convert array item %d: %w", i, err)
			}
			items[i] = converted
		}
		return (&otelv1.InboundLogRecord_AnyValue_builder{
			ArrayValue: (&otelv1.InboundLogRecord_ArrayValue_builder{Values: items}).Build(),
		}).Build(), nil
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		items := make([]*otelv1.InboundLogRecord_KeyValue, 0, len(keys))
		for _, key := range keys {
			converted, err := otelAnyValue(value[key])
			if err != nil {
				return nil, fmt.Errorf("convert object field %q: %w", key, err)
			}
			items = append(items, (&otelv1.InboundLogRecord_KeyValue_builder{
				Key:   &key,
				Value: converted,
			}).Build())
		}
		return (&otelv1.InboundLogRecord_AnyValue_builder{
			KvlistValue: (&otelv1.InboundLogRecord_KeyValueList_builder{Values: items}).Build(),
		}).Build(), nil
	default:
		return nil, fmt.Errorf("unsupported JSON value %T", value)
	}
}

func decodeOTELHexID(value *string, size int) ([]byte, error) {
	if value == nil || *value == "" {
		return nil, nil
	}
	decoded, err := hex.DecodeString(*value)
	if err != nil {
		return nil, fmt.Errorf("decode hex value: %w", err)
	}
	if len(decoded) != size {
		return nil, fmt.Errorf("expected %d bytes, got %d", size, len(decoded))
	}
	return decoded, nil
}

func stringOTELAttribute(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

func eventNameFromURN(eventURN string) string {
	_, eventName, ok := strings.Cut(eventURN, ":")
	for ok {
		eventURN = eventName
		_, eventName, ok = strings.Cut(eventURN, ":")
	}
	return eventURN
}

func otelSeverityNumber(severityText *string) otelv1.InboundLogRecord_SeverityNumber {
	if severityText == nil {
		return otelv1.InboundLogRecord_SEVERITY_NUMBER_UNSPECIFIED
	}
	switch strings.ToUpper(*severityText) {
	case "TRACE":
		return otelv1.InboundLogRecord_SEVERITY_NUMBER_TRACE
	case "DEBUG":
		return otelv1.InboundLogRecord_SEVERITY_NUMBER_DEBUG
	case "INFO":
		return otelv1.InboundLogRecord_SEVERITY_NUMBER_INFO
	case "WARN", "WARNING":
		return otelv1.InboundLogRecord_SEVERITY_NUMBER_WARN
	case "ERROR":
		return otelv1.InboundLogRecord_SEVERITY_NUMBER_ERROR
	case "FATAL":
		return otelv1.InboundLogRecord_SEVERITY_NUMBER_FATAL
	default:
		return otelv1.InboundLogRecord_SEVERITY_NUMBER_UNSPECIFIED
	}
}
