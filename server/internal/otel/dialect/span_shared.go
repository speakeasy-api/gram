package dialect

import otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"

func getOneAttr(span *otelv1.InboundSpan, keys ...string) (key string, value string) {
	for _, desired := range keys {
		for _, kv := range span.GetAttributes() {
			if kv.GetKey() != desired || !kv.GetValue().HasStringValue() {
				continue
			}

			// An empty value states nothing, so keep looking. Without this a
			// present-but-empty first key would shadow a later one that does
			// carry an answer, and the log accessor already skips empties.
			if value := kv.GetValue().GetStringValue(); value != "" {
				return desired, value
			}
		}
	}

	return "", ""
}

func getOneSpanInt64(span *otelv1.InboundSpan, keys ...string) (string, int64) {
	for _, desired := range keys {
		for _, kv := range span.GetAttributes() {
			if kv.GetKey() != desired {
				continue
			}
			if value, ok := scalarInt64(spanScalar(kv.GetValue())); ok {
				return desired, value
			}
		}
	}
	return "", 0
}

func getOneSpanFloat64(span *otelv1.InboundSpan, keys ...string) (string, float64) {
	for _, desired := range keys {
		for _, kv := range span.GetAttributes() {
			if kv.GetKey() != desired {
				continue
			}
			if value, ok := scalarFloat64(spanScalar(kv.GetValue())); ok {
				return desired, value
			}
		}
	}
	return "", 0
}

func spanScalar(value *otelv1.InboundSpan_AnyValue) scalar {
	switch value.WhichValue() {
	case otelv1.InboundSpan_AnyValue_StringValue_case:
		return scalar{kind: kindString, s: value.GetStringValue(), i: 0, f: 0, b: false}
	case otelv1.InboundSpan_AnyValue_IntValue_case:
		return scalar{kind: kindInt, s: "", i: value.GetIntValue(), f: 0, b: false}
	case otelv1.InboundSpan_AnyValue_DoubleValue_case:
		return scalar{kind: kindDouble, s: "", i: 0, f: value.GetDoubleValue(), b: false}
	case otelv1.InboundSpan_AnyValue_BoolValue_case:
		return scalar{kind: kindBool, s: "", i: 0, f: 0, b: value.GetBoolValue()}
	case otelv1.InboundSpan_AnyValue_Value_not_set_case,
		otelv1.InboundSpan_AnyValue_ArrayValue_case,
		otelv1.InboundSpan_AnyValue_KvlistValue_case,
		otelv1.InboundSpan_AnyValue_BytesValue_case:
		return scalar{kind: kindAbsent, s: "", i: 0, f: 0, b: false}
	}
	return scalar{kind: kindAbsent, s: "", i: 0, f: 0, b: false}
}
