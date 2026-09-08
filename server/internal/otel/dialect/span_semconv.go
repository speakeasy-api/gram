package dialect

import (
	"bytes"
	"encoding/json"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/genaiconv"
)

type SemconvSpan struct{}

func (e SemconvSpan) AppliesTo(span *otelv1.InboundSpan) bool {
	return true
}

func (e SemconvSpan) InputContent(span *otelv1.InboundSpan) (string, genaiconv.InputMessages, error) {
	return semconvContent[genaiconv.InputMessages](span, semconvInputMessagesKey)
}

func (e SemconvSpan) OutputContent(span *otelv1.InboundSpan) (string, genaiconv.OutputMessages, error) {
	return semconvContent[genaiconv.OutputMessages](span, semconvOutputMessagesKey)
}

func semconvContent[T any](span *otelv1.InboundSpan, desired string) (string, T, error) {
	var zero T

	for _, kv := range span.GetAttributes() {
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
			encoded, err = json.Marshal(semconvAnyValue(value))
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

func semconvAnyValue(value *otelv1.InboundSpan_AnyValue) any {
	switch value.WhichValue() {
	case otelv1.InboundSpan_AnyValue_Value_not_set_case:
		return nil
	case otelv1.InboundSpan_AnyValue_StringValue_case:
		return value.GetStringValue()
	case otelv1.InboundSpan_AnyValue_BoolValue_case:
		return value.GetBoolValue()
	case otelv1.InboundSpan_AnyValue_IntValue_case:
		return value.GetIntValue()
	case otelv1.InboundSpan_AnyValue_DoubleValue_case:
		return value.GetDoubleValue()
	case otelv1.InboundSpan_AnyValue_ArrayValue_case:
		values := value.GetArrayValue().GetValues()
		result := make([]any, len(values))
		for i, item := range values {
			result[i] = semconvAnyValue(item)
		}
		return result
	case otelv1.InboundSpan_AnyValue_KvlistValue_case:
		values := value.GetKvlistValue().GetValues()
		result := make(map[string]any, len(values))
		for _, item := range values {
			result[item.GetKey()] = semconvAnyValue(item.GetValue())
		}
		return result
	case otelv1.InboundSpan_AnyValue_BytesValue_case:
		return value.GetBytesValue()
	}
	return nil
}

func (e SemconvSpan) SessionID(span *otelv1.InboundSpan) (key string, val string, err error) {
	key, val = getOneAttr(span, "gen_ai.conversation.id")
	return key, val, nil
}

func (e SemconvSpan) ExternalUserEmail(span *otelv1.InboundSpan) (key string, val string, err error) {
	key, val = getOneAttr(span, userEmailKey)
	return key, val, nil
}

func (e SemconvSpan) ExternalUserID(span *otelv1.InboundSpan) (key string, val string, err error) {
	key, val = getOneAttr(span, semconvUserIDKey)
	return key, val, nil
}

func (e SemconvSpan) ResponseID(span *otelv1.InboundSpan) (key string, val string, err error) {
	key, val = getOneAttr(span, "gen_ai.response.id")
	return key, val, nil
}
