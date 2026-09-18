package dialect

import (
	"bytes"
	"encoding/json"
	"math"

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

func (SemconvSpan) Provider(span *otelv1.InboundSpan) (string, string, error) {
	key, value := getOneAttr(span, "gen_ai.provider.name", "gen_ai.system")
	return key, value, nil
}

func (SemconvSpan) Surface(*otelv1.InboundSpan) (string, string, error) {
	return "", "", nil
}

// EventName for a span is the span's own name.
func (SemconvSpan) EventName(span *otelv1.InboundSpan) (string, string, error) {
	if name := span.GetName(); name != "" {
		return "name", name, nil
	}
	return "", "", nil
}

func (SemconvSpan) EventType(span *otelv1.InboundSpan) (string, string, error) {
	key, operation := getOneAttr(span, "gen_ai.operation.name")
	if kind := semconvOperationType(operation); kind != EventTypeUnclassified {
		return key, kind, nil
	}
	return "", "", nil
}

func (SemconvSpan) SubjectID(span *otelv1.InboundSpan) (string, string, error) {
	_, operation := getOneAttr(span, "gen_ai.operation.name")
	switch semconvOperationType(operation) {
	case EventTypeAPIRequest:
		key, value := getOneAttr(span, "gen_ai.response.id")
		return key, value, nil
	case EventTypeToolCall:
		key, value := getOneAttr(span, "gen_ai.tool.call.id")
		return key, value, nil
	}
	return "", "", nil
}

func (SemconvSpan) TurnID(*otelv1.InboundSpan) (string, string, error) {
	return "", "", nil
}

func (SemconvSpan) Model(span *otelv1.InboundSpan) (string, string, error) {
	key, value := getOneAttr(span, "gen_ai.response.model", "gen_ai.request.model")
	return key, value, nil
}

func (SemconvSpan) ToolName(span *otelv1.InboundSpan) (string, string, error) {
	key, value := getOneAttr(span, "gen_ai.tool.name")
	return key, value, nil
}

// Outcome is the span status, in agent vocabulary.
func (SemconvSpan) Outcome(span *otelv1.InboundSpan) (string, string, error) {
	switch span.GetStatus().GetCode() {
	case otelv1.InboundSpan_STATUS_CODE_OK:
		return "status.code", OutcomeOK, nil
	case otelv1.InboundSpan_STATUS_CODE_ERROR:
		return "status.code", OutcomeError, nil
	case otelv1.InboundSpan_STATUS_CODE_UNSPECIFIED:
	}
	return "", "", nil
}

func (SemconvSpan) OutcomeMessage(span *otelv1.InboundSpan) (string, string, error) {
	if span.GetStatus().GetCode() == otelv1.InboundSpan_STATUS_CODE_ERROR && span.GetStatus().GetMessage() != "" {
		return "status.message", span.GetStatus().GetMessage(), nil
	}
	return "", "", nil
}

func (SemconvSpan) Text(*otelv1.InboundSpan) (string, string, error) {
	return "", "", nil
}

// DurationNano is the span duration, clamped rather than wrapped on the
// practically unreachable overflow.
func (SemconvSpan) DurationNano(span *otelv1.InboundSpan) (string, int64, error) {
	start, end := span.GetStartTimeUnixNano(), span.GetEndTimeUnixNano()
	// A zero start is a start the producer never stated, not the epoch.
	// Subtracting it would report the time since 1970 as the duration.
	if start == 0 || end <= start {
		return "", 0, nil
	}
	return "end_time_unix_nano", unixNano(end) - unixNano(start), nil
}

func (SemconvSpan) InputTokens(span *otelv1.InboundSpan) (string, int64, error) {
	key, value := getOneSpanInt64(span, "gen_ai.usage.input_tokens", "gen_ai.usage.prompt_tokens")
	return key, value, nil
}

func (SemconvSpan) OutputTokens(span *otelv1.InboundSpan) (string, int64, error) {
	key, value := getOneSpanInt64(span, "gen_ai.usage.output_tokens", "gen_ai.usage.completion_tokens")
	return key, value, nil
}

func (SemconvSpan) CacheReadTokens(span *otelv1.InboundSpan) (string, int64, error) {
	key, value := getOneSpanInt64(span, "gen_ai.usage.cache_read.input_tokens")
	return key, value, nil
}

func (SemconvSpan) CacheWriteTokens(span *otelv1.InboundSpan) (string, int64, error) {
	key, value := getOneSpanInt64(span, "gen_ai.usage.cache_creation.input_tokens")
	return key, value, nil
}

func (SemconvSpan) CostUSD(span *otelv1.InboundSpan) (string, float64, error) {
	key, value := getOneSpanFloat64(span, "gen_ai.usage.cost")
	return key, value, nil
}

func (SemconvSpan) QuerySource(*otelv1.InboundSpan) (string, string, error) {
	return "", "", nil
}

func (SemconvSpan) SkillName(*otelv1.InboundSpan) (string, string, error) {
	return "", "", nil
}

func (SemconvSpan) AgentName(span *otelv1.InboundSpan) (string, string, error) {
	key, value := getOneAttr(span, "gen_ai.agent.name")
	return key, value, nil
}

func (SemconvSpan) MCPServerName(*otelv1.InboundSpan) (string, string, error) {
	return "", "", nil
}

func (SemconvSpan) MCPToolName(*otelv1.InboundSpan) (string, string, error) {
	return "", "", nil
}

func (SemconvSpan) ExternalOrgID(*otelv1.InboundSpan) (string, string, error) {
	return "", "", nil
}

// unixNano converts an OTLP fixed64 nanosecond timestamp to Int64, clamping
// the practically unreachable overflow instead of wrapping negative.
func unixNano(v uint64) int64 {
	if v > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(v)
}
