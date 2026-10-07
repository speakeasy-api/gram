package gramotel

import (
	"fmt"
	"math"
	"time"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

// maxValueDepth bounds how deeply nested a log value may be. Real records
// nest two or three levels; the bound stops a self-referential value from
// recursing without end.
const maxValueDepth = 32

// inboundFromSDK converts an SDK log record into the inbound record type,
// with its resource and instrumentation scope, leaving record id, observed
// time and provenance to the caller.
func inboundFromSDK(record *sdklog.Record) (*otelv1.InboundLogRecord, error) {
	body, err := anyValue(record.Body(), 0)
	if err != nil {
		return nil, fmt.Errorf("convert body: %w", err)
	}

	attributes := make([]*otelv1.InboundLogRecord_KeyValue, 0, record.AttributesLen())
	var walkErr error
	record.WalkAttributes(func(kv log.KeyValue) bool {
		value, err := anyValue(kv.Value, 0)
		if err != nil {
			walkErr = fmt.Errorf("convert attribute %q: %w", kv.Key, err)
			return false
		}
		key := kv.Key
		attributes = append(attributes, (&otelv1.InboundLogRecord_KeyValue_builder{Key: &key, Value: value}).Build())
		return true
	})
	if walkErr != nil {
		return nil, walkErr
	}

	severity := severityNumber(record.Severity())
	severityText := record.SeverityText()
	timestamp := unixNano(record.Timestamp())
	flags := uint32(record.TraceFlags())
	dropped := uint32(min(max(record.DroppedAttributes(), 0), math.MaxUint32))
	builder := &otelv1.InboundLogRecord_builder{
		TimeUnixNano:           &timestamp,
		SeverityNumber:         &severity,
		SeverityText:           &severityText,
		Body:                   body,
		Attributes:             attributes,
		DroppedAttributesCount: &dropped,
		Flags:                  &flags,
	}
	if name := record.EventName(); name != "" {
		builder.EventName = &name
	}
	if traceID := record.TraceID(); traceID.IsValid() {
		builder.TraceId = traceID[:]
	}
	if spanID := record.SpanID(); spanID.IsValid() {
		builder.SpanId = spanID[:]
	}

	if res := record.Resource(); res != nil {
		resourceAttributes, err := attributeKeyValues(res.Attributes())
		if err != nil {
			return nil, fmt.Errorf("convert resource: %w", err)
		}
		builder.Resource = (&otelv1.InboundLogRecord_Resource_builder{Attributes: resourceAttributes}).Build()
		if url := res.SchemaURL(); url != "" {
			builder.ResourceSchemaUrl = &url
		}
	}

	scope := record.InstrumentationScope()
	scopeAttributes, err := attributeKeyValues(scope.Attributes.ToSlice())
	if err != nil {
		return nil, fmt.Errorf("convert scope: %w", err)
	}
	scopeName, scopeVersion := scope.Name, scope.Version
	builder.Scope = (&otelv1.InboundLogRecord_InstrumentationScope_builder{
		Name:       &scopeName,
		Version:    &scopeVersion,
		Attributes: scopeAttributes,
	}).Build()
	if url := scope.SchemaURL; url != "" {
		builder.ScopeSchemaUrl = &url
	}

	return builder.Build(), nil
}

// severityNumber maps the SDK's severity onto OTLP's, which share one
// numbering from 1 (trace) to 24 (fatal4); anything outside it is unset.
func severityNumber(severity log.Severity) otelv1.InboundLogRecord_SeverityNumber {
	if severity < log.SeverityTrace1 || severity > log.SeverityFatal4 {
		return otelv1.InboundLogRecord_SEVERITY_NUMBER_UNSPECIFIED
	}
	return otelv1.InboundLogRecord_SeverityNumber(severity)
}

// unixNano converts a time to OTLP's unsigned nanoseconds; the zero time and
// anything before the epoch are "not stated".
func unixNano(t time.Time) uint64 {
	if t.IsZero() {
		return 0
	}
	return uint64(max(t.UnixNano(), 0))
}

func anyValue(value log.Value, depth int) (*otelv1.InboundLogRecord_AnyValue, error) {
	if depth > maxValueDepth {
		return nil, fmt.Errorf("value nested deeper than %d levels", maxValueDepth)
	}
	switch value.Kind() {
	case log.KindEmpty:
		return nil, nil
	case log.KindBool:
		v := value.AsBool()
		return (&otelv1.InboundLogRecord_AnyValue_builder{BoolValue: &v}).Build(), nil
	case log.KindInt64:
		v := value.AsInt64()
		return (&otelv1.InboundLogRecord_AnyValue_builder{IntValue: &v}).Build(), nil
	case log.KindFloat64:
		v := value.AsFloat64()
		return (&otelv1.InboundLogRecord_AnyValue_builder{DoubleValue: &v}).Build(), nil
	case log.KindString:
		v := value.AsString()
		return (&otelv1.InboundLogRecord_AnyValue_builder{StringValue: &v}).Build(), nil
	case log.KindBytes:
		return (&otelv1.InboundLogRecord_AnyValue_builder{BytesValue: value.AsBytes()}).Build(), nil
	case log.KindSlice:
		items := value.AsSlice()
		values := make([]*otelv1.InboundLogRecord_AnyValue, 0, len(items))
		for _, item := range items {
			converted, err := anyValue(item, depth+1)
			if err != nil {
				return nil, err
			}
			values = append(values, converted)
		}
		return (&otelv1.InboundLogRecord_AnyValue_builder{
			ArrayValue: (&otelv1.InboundLogRecord_ArrayValue_builder{Values: values}).Build(),
		}).Build(), nil
	case log.KindMap:
		entries := value.AsMap()
		values := make([]*otelv1.InboundLogRecord_KeyValue, 0, len(entries))
		for _, entry := range entries {
			converted, err := anyValue(entry.Value, depth+1)
			if err != nil {
				return nil, err
			}
			key := entry.Key
			values = append(values, (&otelv1.InboundLogRecord_KeyValue_builder{Key: &key, Value: converted}).Build())
		}
		return (&otelv1.InboundLogRecord_AnyValue_builder{
			KvlistValue: (&otelv1.InboundLogRecord_KeyValueList_builder{Values: values}).Build(),
		}).Build(), nil
	default:
		return nil, fmt.Errorf("unsupported log value kind %s", value.Kind())
	}
}

// attributeKeyValues converts resource and scope attributes, which the SDK
// holds as attribute.KeyValue rather than log.KeyValue.
func attributeKeyValues(attrs []attribute.KeyValue) ([]*otelv1.InboundLogRecord_KeyValue, error) {
	out := make([]*otelv1.InboundLogRecord_KeyValue, 0, len(attrs))
	for _, kv := range attrs {
		converted, err := anyValue(log.ValueFromAttribute(kv.Value), 0)
		if err != nil {
			return nil, fmt.Errorf("convert attribute %q: %w", kv.Key, err)
		}
		key := string(kv.Key)
		out = append(out, (&otelv1.InboundLogRecord_KeyValue_builder{Key: &key, Value: converted}).Build())
	}
	return out, nil
}
