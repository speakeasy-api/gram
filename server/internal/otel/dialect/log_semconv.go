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
	return semconvLogContent[genaiconv.InputMessages](record, semconvInputMessagesKey)
}

func (SemconvLog) OutputContent(record *otelv1.InboundLogRecord) (string, genaiconv.OutputMessages, error) {
	return semconvLogContent[genaiconv.OutputMessages](record, semconvOutputMessagesKey)
}

func (SemconvLog) SessionID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, "gen_ai.conversation.id")
	return key, value, nil
}

func (SemconvLog) ExternalUserEmail(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, userEmailKey)
	return key, value, nil
}

func (SemconvLog) ExternalUserID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, semconvUserIDKey)
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

// semconvOperationType maps gen_ai.operation.name onto the agent vocabulary.
func semconvOperationType(operation string) string {
	switch operation {
	case "chat", "generate_content", "text_completion", "embeddings",
		"image_generation", "create_agent", "invoke_agent":
		return EventTypeAPIRequest
	case "execute_tool":
		return EventTypeToolCall
	}
	return EventTypeUnclassified
}

func (SemconvLog) Provider(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, "gen_ai.provider.name")
	if key == "" {
		key, value = getOneLogAttr(record, "gen_ai.system")
	}
	return key, value, nil
}

// Surface: the semantic conventions do not say which agent was behind a
// request.
func (SemconvLog) Surface(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

func (SemconvLog) EventName(record *otelv1.InboundLogRecord) (string, string, error) {
	key, name := logRawEventName(record)
	return key, name, nil
}

func (SemconvLog) EventType(record *otelv1.InboundLogRecord) (string, string, error) {
	key, operation := getOneLogAttr(record, "gen_ai.operation.name")
	if kind := semconvOperationType(operation); kind != EventTypeUnclassified {
		return key, kind, nil
	}
	return "", "", nil
}

func (SemconvLog) SubjectID(record *otelv1.InboundLogRecord) (string, string, error) {
	_, operation := getOneLogAttr(record, "gen_ai.operation.name")
	switch semconvOperationType(operation) {
	case EventTypeAPIRequest:
		key, value := getOneLogAttr(record, "gen_ai.response.id")
		return key, value, nil
	case EventTypeToolCall:
		key, value := getOneLogAttr(record, "gen_ai.tool.call.id")
		return key, value, nil
	}
	return "", "", nil
}

func (SemconvLog) TurnID(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

func (SemconvLog) Model(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, "gen_ai.response.model")
	if key == "" {
		key, value = getOneLogAttr(record, "gen_ai.request.model")
	}
	return key, value, nil
}

func (SemconvLog) ToolName(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, "gen_ai.tool.name")
	return key, value, nil
}

func (SemconvLog) Outcome(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

func (SemconvLog) OutcomeMessage(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

// Text is the record in words. Semconv states no text field of its own, but a
// producer that wrote words into the body meant them, and the row builder only
// falls back to the body for records nothing classified, so a record semconv
// classified would lose them.
//
// Only for a record semconv itself classified. A record another dialect
// claimed reaches this through the fallback, and that dialect has already
// decided what its words are: Claude Code, for one, puts them in attributes
// and leaves a body this has no business promoting.
func (SemconvLog) Text(record *otelv1.InboundLogRecord) (string, string, error) {
	if _, operation := getOneLogAttr(record, "gen_ai.operation.name"); semconvOperationType(operation) == EventTypeUnclassified {
		return "", "", nil
	}

	body := record.GetBody()
	if !body.HasStringValue() {
		return "", "", nil
	}

	value := body.GetStringValue()
	_, name := logRawEventName(record)
	if value == "" || BodyRepeatsEventName(value, name) {
		return "", "", nil
	}

	return "body", value, nil
}

func (SemconvLog) DurationNano(*otelv1.InboundLogRecord) (string, int64, error) {
	return "", 0, nil
}

func (SemconvLog) InputTokens(record *otelv1.InboundLogRecord) (string, int64, error) {
	key, value := getOneLogInt64(record, "gen_ai.usage.input_tokens", "gen_ai.usage.prompt_tokens")
	return key, value, nil
}

func (SemconvLog) OutputTokens(record *otelv1.InboundLogRecord) (string, int64, error) {
	key, value := getOneLogInt64(record, "gen_ai.usage.output_tokens", "gen_ai.usage.completion_tokens")
	return key, value, nil
}

func (SemconvLog) CacheReadTokens(record *otelv1.InboundLogRecord) (string, int64, error) {
	key, value := getOneLogInt64(record, "gen_ai.usage.cache_read.input_tokens")
	return key, value, nil
}

func (SemconvLog) CacheWriteTokens(record *otelv1.InboundLogRecord) (string, int64, error) {
	key, value := getOneLogInt64(record, "gen_ai.usage.cache_creation.input_tokens")
	return key, value, nil
}

func (SemconvLog) CostUSD(record *otelv1.InboundLogRecord) (string, float64, error) {
	key, value := getOneLogFloat64(record, "gen_ai.usage.cost")
	return key, value, nil
}

func (SemconvLog) QuerySource(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

func (SemconvLog) SkillName(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

func (SemconvLog) AgentName(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, "gen_ai.agent.name")
	return key, value, nil
}

func (SemconvLog) MCPServerName(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

func (SemconvLog) MCPToolName(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

func (SemconvLog) ExternalOrgID(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}
