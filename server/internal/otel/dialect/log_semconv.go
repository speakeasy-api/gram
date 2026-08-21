package dialect

import (
	"bytes"
	"encoding/json"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/genaiconv"
)

type SemconvLog struct{}

func (SemconvLog) AppliesTo(*otelv1.InboundLogRecord) bool { return true }

func (SemconvLog) InputContent(record *otelv1.InboundLogRecord) (string, genaiconv.InputMessages, error) {
	return semconvLogContent[genaiconv.InputMessages](record, "gen_ai.input.messages")
}

func (SemconvLog) OutputContent(record *otelv1.InboundLogRecord) (string, genaiconv.OutputMessages, error) {
	return semconvLogContent[genaiconv.OutputMessages](record, "gen_ai.output.messages")
}

func (SemconvLog) SessionID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, "gen_ai.conversation.id")
	return key, value, nil
}

func (SemconvLog) ExternalUserEmail(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, "user.email")
	return key, value, nil
}

func (SemconvLog) ExternalUserID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, "user.id")
	return key, value, nil
}

func (SemconvLog) ResponseID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, "gen_ai.response.id")
	return key, value, nil
}

func semconvLogContent[T any](record *otelv1.InboundLogRecord, desired string) (string, T, error) {
	var zero T

	for _, kv := range record.GetAttributes() {
		if kv.GetKey() != desired {
			continue
		}

		value := kv.GetValue()
		var encoded []byte
		switch {
		case value.HasStringValue():
			encoded = []byte(value.GetStringValue())
		case value.HasArrayValue():
			var err error
			encoded, err = json.Marshal(semconvLogAnyValue(value))
			if err != nil {
				continue
			}
		default:
			continue
		}

		encoded = bytes.TrimSpace(encoded)
		if len(encoded) == 0 || encoded[0] != '[' {
			continue
		}

		var messages T
		if err := json.Unmarshal(encoded, &messages); err != nil {
			continue
		}

		return desired, messages, nil
	}

	return "", zero, nil
}

func semconvLogAnyValue(value *otelv1.InboundLogRecord_AnyValue) any {
	switch value.WhichValue() {
	case otelv1.InboundLogRecord_AnyValue_Value_not_set_case:
		return nil
	case otelv1.InboundLogRecord_AnyValue_StringValue_case:
		return value.GetStringValue()
	case otelv1.InboundLogRecord_AnyValue_BoolValue_case:
		return value.GetBoolValue()
	case otelv1.InboundLogRecord_AnyValue_IntValue_case:
		return value.GetIntValue()
	case otelv1.InboundLogRecord_AnyValue_DoubleValue_case:
		return value.GetDoubleValue()
	case otelv1.InboundLogRecord_AnyValue_ArrayValue_case:
		values := value.GetArrayValue().GetValues()
		result := make([]any, len(values))
		for i, item := range values {
			result[i] = semconvLogAnyValue(item)
		}
		return result
	case otelv1.InboundLogRecord_AnyValue_KvlistValue_case:
		values := value.GetKvlistValue().GetValues()
		result := make(map[string]any, len(values))
		for _, item := range values {
			result[item.GetKey()] = semconvLogAnyValue(item.GetValue())
		}
		return result
	case otelv1.InboundLogRecord_AnyValue_BytesValue_case:
		return value.GetBytesValue()
	}

	return nil
}
