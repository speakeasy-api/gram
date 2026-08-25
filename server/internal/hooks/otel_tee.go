package hooks

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math"
	"time"

	"github.com/google/uuid"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	otelsvc "github.com/speakeasy-api/gram/server/internal/otel"
)

const (
	// otelTeeAckAwaitTimeout bounds the detached goroutine that drains
	// publish acks for one teed export.
	otelTeeAckAwaitTimeout = 10 * time.Second

	// otelTeeMaxAnyValueDepth caps recursion when converting untyped
	// OTLP/JSON array and kvlist values; deeper values fall back to their
	// JSON encoding.
	otelTeeMaxAnyValueDepth = 10

	// otelTeeTraceIDBytes and otelTeeSpanIDBytes are the OTLP identifier
	// widths. Hex ids that decode to any other width are dropped, matching
	// what the native ingest edge's record validation rejects.
	otelTeeTraceIDBytes = 16
	otelTeeSpanIDBytes  = 8
)

// teeOTELLogsToEventFeed republishes an OTLP logs export received on the
// deprecated hooks endpoint into the OTel event feed pipeline (the
// gram.otel.v1.InboundLogRecord topic), so hooks traffic is visible in the
// Event Feed while producers migrate to /otel/v1/logs. Best-effort and
// non-blocking: publish acks drain on a detached goroutine and a failure
// never affects the hooks response — a record dropped here simply does not
// appear in the feed.
func (s *Service) teeOTELLogsToEventFeed(ctx context.Context, payload *gen.LogsPayload, orgID string, projectID string) {
	if s.otelLogPublisher == nil || payload == nil {
		return
	}

	provenance := (&otelv1.InboundLogRecord_Provenance_builder{
		Source:         new(otelsvc.ProvenanceSource),
		OrganizationId: &orgID,
		ProjectId:      &projectID,
	}).Build()

	records := inboundLogRecordsFromHooksExport(payload, provenance, s.now())
	if len(records) == 0 {
		return
	}

	// The hooks response never waits on the tee: detach cancellation so a
	// client disconnect cannot abort the publish or the ack drain, and bound
	// the drain with its own timeout instead.
	ctx = context.WithoutCancel(ctx)

	results := make([]gcp.PublishResult, 0, len(records))
	for _, record := range records {
		if err := otelsvc.ValidateInboundLogRecord(record); err != nil {
			s.logger.WarnContext(ctx, "skipping hooks OTEL log record in event feed tee", attr.SlogError(err))
			continue
		}
		results = append(results, s.otelLogPublisher.Publish(ctx, record))
	}

	s.otelTeeDrains.Go(func() {

		ctx, cancel := context.WithTimeout(ctx, otelTeeAckAwaitTimeout)
		defer cancel()

		var firstErr error
		failed := 0
		for _, result := range results {
			if _, err := result.Get(ctx); err != nil {
				failed++
				if firstErr == nil {
					firstErr = err
				}
			}
		}
		if firstErr != nil {
			s.logger.ErrorContext(ctx, "publish hooks OTEL logs to event feed pipeline",
				attr.SlogError(firstErr),
				attr.SlogTelemetryPublishFailedCount(failed),
			)
		}
	})
}

// inboundLogRecordsFromHooksExport converts a decoded OTLP/JSON logs export
// into inbound pipeline records, mirroring the contract the native ingest
// edge applies in decodeOTLPLogExport: a fresh record id per record, observed
// time stamped when the producer sent none, and resource, scope, and
// provenance attached to every record. The hooks payload models no schema
// URLs, severity, or flags, so those stay unset.
func inboundLogRecordsFromHooksExport(payload *gen.LogsPayload, provenance *otelv1.InboundLogRecord_Provenance, now time.Time) []*otelv1.InboundLogRecord {
	records := make([]*otelv1.InboundLogRecord, 0)

	for _, resourceLog := range payload.ResourceLogs {
		if resourceLog == nil {
			continue
		}

		var resource *otelv1.InboundLogRecord_Resource
		if resourceLog.Resource != nil {
			resource = (&otelv1.InboundLogRecord_Resource_builder{
				Attributes:             inboundResourceKeyValues(resourceLog.Resource.Attributes),
				DroppedAttributesCount: inboundDroppedCount(resourceLog.Resource.DroppedAttributesCount),
			}).Build()
		}

		for _, scopeLog := range resourceLog.ScopeLogs {
			if scopeLog == nil {
				continue
			}

			var scope *otelv1.InboundLogRecord_InstrumentationScope
			if scopeLog.Scope != nil {
				scope = (&otelv1.InboundLogRecord_InstrumentationScope_builder{
					Name:                   scopeLog.Scope.Name,
					Version:                scopeLog.Scope.Version,
					Attributes:             nil,
					DroppedAttributesCount: nil,
				}).Build()
			}

			for _, logRecord := range scopeLog.LogRecords {
				if logRecord == nil {
					continue
				}
				records = append(records, inboundLogRecordFromHooksRecord(logRecord, resource, scope, provenance, now))
			}
		}
	}

	return records
}

func inboundLogRecordFromHooksRecord(
	record *gen.OTELLogRecord,
	resource *otelv1.InboundLogRecord_Resource,
	scope *otelv1.InboundLogRecord_InstrumentationScope,
	provenance *otelv1.InboundLogRecord_Provenance,
	now time.Time,
) *otelv1.InboundLogRecord {
	var timeNano *uint64
	if record.TimeUnixNano != nil {
		if n, ok := parseUnixNanoString(*record.TimeUnixNano); ok {
			v := positiveNanoToUint64(n)
			timeNano = &v
		}
	}

	// OTLP receivers stamp observed time when the producer did not. Stamping
	// before the first publish keeps the value stable across Pub/Sub
	// redeliveries.
	observedNano := positiveNanoToUint64(now.UnixNano())
	if record.ObservedTimeUnixNano != nil {
		if n, ok := parseUnixNanoString(*record.ObservedTimeUnixNano); ok {
			observedNano = positiveNanoToUint64(n)
		}
	}

	var body *otelv1.InboundLogRecord_AnyValue
	if record.Body != nil && record.Body.StringValue != nil {
		body = (&otelv1.InboundLogRecord_AnyValue_builder{StringValue: record.Body.StringValue}).Build()
	}

	return (&otelv1.InboundLogRecord_builder{
		RecordId:               new(uuid.NewString()),
		TimeUnixNano:           timeNano,
		ObservedTimeUnixNano:   &observedNano,
		TraceId:                inboundEventID(record.TraceID, otelTeeTraceIDBytes),
		SpanId:                 inboundEventID(record.SpanID, otelTeeSpanIDBytes),
		Body:                   body,
		Attributes:             inboundLogKeyValues(record.Attributes),
		DroppedAttributesCount: inboundDroppedCount(record.DroppedAttributesCount),
		SeverityNumber:         nil,
		SeverityText:           nil,
		Flags:                  nil,
		EventName:              nil,
		Resource:               resource,
		ResourceSchemaUrl:      nil,
		Scope:                  scope,
		ScopeSchemaUrl:         nil,
		Provenance:             provenance,
	}).Build()
}

// positiveNanoToUint64 widens a nanosecond timestamp to the OTLP fixed64
// form, mapping non-positive values to zero.
func positiveNanoToUint64(n int64) uint64 {
	if n <= 0 {
		return 0
	}
	return uint64(n)
}

// inboundEventID decodes a hex trace or span id into bytes, returning nil for
// unset ids and for values that do not decode to exactly the expected width.
func inboundEventID(raw *string, size int) []byte {
	if raw == nil || *raw == "" {
		return nil
	}
	decoded, err := hex.DecodeString(*raw)
	if err != nil || len(decoded) != size {
		return nil
	}
	return decoded
}

func inboundDroppedCount(count *int) *uint32 {
	if count == nil || *count <= 0 || *count > math.MaxUint32 {
		return nil
	}
	v := uint32(*count)
	return &v
}

func inboundLogKeyValues(attrs []*gen.OTELAttribute) []*otelv1.InboundLogRecord_KeyValue {
	out := make([]*otelv1.InboundLogRecord_KeyValue, 0, len(attrs))
	for _, item := range attrs {
		if item == nil || item.Value == nil {
			continue
		}
		value := inboundAnyValue(item.Value, 0)
		if value == nil {
			continue
		}
		out = append(out, (&otelv1.InboundLogRecord_KeyValue_builder{
			Key:   &item.Key,
			Value: value,
		}).Build())
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func inboundResourceKeyValues(attrs []*gen.OTELResourceAttribute) []*otelv1.InboundLogRecord_KeyValue {
	out := make([]*otelv1.InboundLogRecord_KeyValue, 0, len(attrs))
	for _, item := range attrs {
		if item == nil || item.Value == nil {
			continue
		}
		value := inboundAnyValue(item.Value, 0)
		if value == nil {
			continue
		}
		out = append(out, (&otelv1.InboundLogRecord_KeyValue_builder{
			Key:   &item.Key,
			Value: value,
		}).Build())
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// inboundAnyValue converts one decoded OTLP/JSON attribute value into its
// typed proto form. Values the converter cannot map (unparsable ints, exotic
// array/kvlist shapes) fall back to their JSON encoding as a string value so
// they survive the tee instead of being dropped. Returns nil only for a value
// with no recognizable kind at all.
func inboundAnyValue(value *gen.OTELAttributeValue, depth int) *otelv1.InboundLogRecord_AnyValue {
	switch {
	case value.StringValue != nil:
		return (&otelv1.InboundLogRecord_AnyValue_builder{StringValue: value.StringValue}).Build()
	case value.IntValue != nil:
		if n, ok := parseLooseInt64(value.IntValue); ok {
			return (&otelv1.InboundLogRecord_AnyValue_builder{IntValue: &n}).Build()
		}
		return jsonFallbackAnyValue(value.IntValue)
	case value.BoolValue != nil:
		return (&otelv1.InboundLogRecord_AnyValue_builder{BoolValue: value.BoolValue}).Build()
	case value.DoubleValue != nil:
		return (&otelv1.InboundLogRecord_AnyValue_builder{DoubleValue: value.DoubleValue}).Build()
	case value.ArrayValue != nil:
		return inboundArrayAnyValue(value.ArrayValue, depth)
	case value.KvlistValue != nil:
		return inboundKvlistAnyValue(value.KvlistValue, depth)
	case value.BytesValue != nil:
		if decoded, err := base64.StdEncoding.DecodeString(*value.BytesValue); err == nil {
			return (&otelv1.InboundLogRecord_AnyValue_builder{BytesValue: decoded}).Build()
		}
		return (&otelv1.InboundLogRecord_AnyValue_builder{StringValue: value.BytesValue}).Build()
	default:
		return nil
	}
}

// inboundLooseAnyValue converts a raw JSON-decoded OTLP AnyValue object —
// the shape found inside untyped array and kvlist passthroughs, e.g.
// {"stringValue": "x"} or {"intValue": "42"}.
func inboundLooseAnyValue(value any, depth int) *otelv1.InboundLogRecord_AnyValue {
	if depth >= otelTeeMaxAnyValueDepth {
		return jsonFallbackAnyValue(value)
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return jsonFallbackAnyValue(value)
	}
	if s, ok := obj["stringValue"].(string); ok {
		return (&otelv1.InboundLogRecord_AnyValue_builder{StringValue: &s}).Build()
	}
	if raw, ok := obj["intValue"]; ok {
		if n, ok := parseLooseInt64(raw); ok {
			return (&otelv1.InboundLogRecord_AnyValue_builder{IntValue: &n}).Build()
		}
		return jsonFallbackAnyValue(value)
	}
	if b, ok := obj["boolValue"].(bool); ok {
		return (&otelv1.InboundLogRecord_AnyValue_builder{BoolValue: &b}).Build()
	}
	if f, ok := obj["doubleValue"].(float64); ok {
		return (&otelv1.InboundLogRecord_AnyValue_builder{DoubleValue: &f}).Build()
	}
	if s, ok := obj["bytesValue"].(string); ok {
		if decoded, err := base64.StdEncoding.DecodeString(s); err == nil {
			return (&otelv1.InboundLogRecord_AnyValue_builder{BytesValue: decoded}).Build()
		}
		return (&otelv1.InboundLogRecord_AnyValue_builder{StringValue: &s}).Build()
	}
	if nested, ok := obj["arrayValue"]; ok {
		return inboundArrayAnyValue(nested, depth)
	}
	if nested, ok := obj["kvlistValue"]; ok {
		return inboundKvlistAnyValue(nested, depth)
	}
	return jsonFallbackAnyValue(value)
}

func inboundArrayAnyValue(value any, depth int) *otelv1.InboundLogRecord_AnyValue {
	if depth >= otelTeeMaxAnyValueDepth {
		return jsonFallbackAnyValue(value)
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return jsonFallbackAnyValue(value)
	}
	var items []any
	if raw, exists := obj["values"]; exists {
		items, ok = raw.([]any)
		if !ok {
			return jsonFallbackAnyValue(value)
		}
	}
	values := make([]*otelv1.InboundLogRecord_AnyValue, 0, len(items))
	for _, item := range items {
		converted := inboundLooseAnyValue(item, depth+1)
		if converted == nil {
			converted = &otelv1.InboundLogRecord_AnyValue{}
		}
		values = append(values, converted)
	}
	return (&otelv1.InboundLogRecord_AnyValue_builder{
		ArrayValue: (&otelv1.InboundLogRecord_ArrayValue_builder{Values: values}).Build(),
	}).Build()
}

func inboundKvlistAnyValue(value any, depth int) *otelv1.InboundLogRecord_AnyValue {
	if depth >= otelTeeMaxAnyValueDepth {
		return jsonFallbackAnyValue(value)
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return jsonFallbackAnyValue(value)
	}
	var entries []any
	if raw, exists := obj["values"]; exists {
		entries, ok = raw.([]any)
		if !ok {
			return jsonFallbackAnyValue(value)
		}
	}
	values := make([]*otelv1.InboundLogRecord_KeyValue, 0, len(entries))
	for _, item := range entries {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		key, _ := entry["key"].(string)
		values = append(values, (&otelv1.InboundLogRecord_KeyValue_builder{
			Key:   &key,
			Value: inboundLooseAnyValue(entry["value"], depth+1),
		}).Build())
	}
	return (&otelv1.InboundLogRecord_AnyValue_builder{
		KvlistValue: (&otelv1.InboundLogRecord_KeyValueList_builder{Values: values}).Build(),
	}).Build()
}

// jsonFallbackAnyValue renders a value this converter cannot map onto a typed
// OTLP value as its JSON encoding.
func jsonFallbackAnyValue(value any) *otelv1.InboundLogRecord_AnyValue {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	s := string(encoded)
	return (&otelv1.InboundLogRecord_AnyValue_builder{StringValue: &s}).Build()
}
