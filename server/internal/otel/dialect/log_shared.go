package dialect

import (
	"strconv"
	"strings"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
)

func getOneLogAttr(record *otelv1.InboundLogRecord, desired string) (string, string) {
	for _, kv := range record.GetAttributes() {
		if kv.GetKey() != desired || !kv.GetValue().HasStringValue() {
			continue
		}
		if value := kv.GetValue().GetStringValue(); value != "" {
			return desired, value
		}
	}
	return "", ""
}

// getOneLogAttrAny is getOneLogAttr for producers that stringify numbers
// unevenly: the first of keys that is present as a string, integer, double
// or boolean is rendered as a string.
func getOneLogAttrAny(record *otelv1.InboundLogRecord, keys ...string) (string, string) {
	for _, desired := range keys {
		for _, kv := range record.GetAttributes() {
			if kv.GetKey() != desired {
				continue
			}
			if value, ok := scalarString(logScalar(kv.GetValue())); ok {
				return desired, value
			}
		}
	}
	return "", ""
}

// getOneLogInt64 reads the first of keys that parses as an integer. A
// stringified number counts; a value that is present but unparseable does
// not, so the caller sees absent rather than zero-by-accident.
func getOneLogInt64(record *otelv1.InboundLogRecord, keys ...string) (string, int64) {
	for _, desired := range keys {
		for _, kv := range record.GetAttributes() {
			if kv.GetKey() != desired {
				continue
			}
			if value, ok := scalarInt64(logScalar(kv.GetValue())); ok {
				return desired, value
			}
		}
	}
	return "", 0
}

func getOneLogFloat64(record *otelv1.InboundLogRecord, keys ...string) (string, float64) {
	for _, desired := range keys {
		for _, kv := range record.GetAttributes() {
			if kv.GetKey() != desired {
				continue
			}
			if value, ok := scalarFloat64(logScalar(kv.GetValue())); ok {
				return desired, value
			}
		}
	}
	return "", 0
}

func getOneLogBool(record *otelv1.InboundLogRecord, keys ...string) (string, bool, bool) {
	for _, desired := range keys {
		for _, kv := range record.GetAttributes() {
			if kv.GetKey() != desired {
				continue
			}
			if value, ok := scalarBool(logScalar(kv.GetValue())); ok {
				return desired, value, true
			}
		}
	}
	return "", false, false
}

// logRawEventName is the producer's own name for a log record: the OTLP
// event name field first, then the event.name attribute older exporters use.
func logRawEventName(record *otelv1.InboundLogRecord) (string, string) {
	if name := record.GetEventName(); name != "" {
		return "event_name", name
	}
	return getOneLogAttr(record, "event.name")
}

// scalar is one attribute value with its wire type kept, so numeric and
// boolean reads can be tolerant of producers that stringify their numbers.
type scalar struct {
	kind scalarKind
	s    string
	i    int64
	f    float64
	b    bool
}

type scalarKind uint8

const (
	kindAbsent scalarKind = iota
	kindString
	kindInt
	kindDouble
	kindBool
)

func logScalar(value *otelv1.InboundLogRecord_AnyValue) scalar {
	switch value.WhichValue() {
	case otelv1.InboundLogRecord_AnyValue_StringValue_case:
		return scalar{kind: kindString, s: value.GetStringValue(), i: 0, f: 0, b: false}
	case otelv1.InboundLogRecord_AnyValue_IntValue_case:
		return scalar{kind: kindInt, s: "", i: value.GetIntValue(), f: 0, b: false}
	case otelv1.InboundLogRecord_AnyValue_DoubleValue_case:
		return scalar{kind: kindDouble, s: "", i: 0, f: value.GetDoubleValue(), b: false}
	case otelv1.InboundLogRecord_AnyValue_BoolValue_case:
		return scalar{kind: kindBool, s: "", i: 0, f: 0, b: value.GetBoolValue()}
	case otelv1.InboundLogRecord_AnyValue_Value_not_set_case,
		otelv1.InboundLogRecord_AnyValue_ArrayValue_case,
		otelv1.InboundLogRecord_AnyValue_KvlistValue_case,
		otelv1.InboundLogRecord_AnyValue_BytesValue_case:
		return scalar{kind: kindAbsent, s: "", i: 0, f: 0, b: false}
	}
	return scalar{kind: kindAbsent, s: "", i: 0, f: 0, b: false}
}

func scalarString(v scalar) (string, bool) {
	switch v.kind {
	case kindString:
		return v.s, v.s != ""
	case kindInt:
		return strconv.FormatInt(v.i, 10), true
	case kindDouble:
		return strconv.FormatFloat(v.f, 'f', -1, 64), true
	case kindBool:
		return strconv.FormatBool(v.b), true
	case kindAbsent:
	}
	return "", false
}

func scalarInt64(v scalar) (int64, bool) {
	switch v.kind {
	case kindInt:
		return v.i, true
	case kindDouble:
		return int64(v.f), true
	case kindString:
		text := strings.TrimSpace(v.s)
		if text == "" {
			return 0, false
		}
		if parsed, err := strconv.ParseInt(text, 10, 64); err == nil {
			return parsed, true
		}
		if parsed, err := strconv.ParseFloat(text, 64); err == nil {
			return int64(parsed), true
		}
	case kindBool, kindAbsent:
	}
	return 0, false
}

func scalarFloat64(v scalar) (float64, bool) {
	switch v.kind {
	case kindDouble:
		return v.f, true
	case kindInt:
		return float64(v.i), true
	case kindString:
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(v.s), 64); err == nil {
			return parsed, true
		}
	case kindBool, kindAbsent:
	}
	return 0, false
}

func scalarBool(v scalar) (bool, bool) {
	switch v.kind {
	case kindBool:
		return v.b, true
	case kindString:
		if parsed, err := strconv.ParseBool(strings.TrimSpace(v.s)); err == nil {
			return parsed, true
		}
	case kindInt:
		return v.i != 0, true
	case kindDouble:
		return v.f != 0, true
	case kindAbsent:
	}
	return false, false
}
