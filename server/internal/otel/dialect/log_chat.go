package dialect

import (
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/genaiconv"
)

// ChatLogScope is the instrumentation scope the chat message bridge logs
// under. It is chatevents.Scope; importing that package here would be a
// cycle, so the string is repeated and a test there pins the two together.
const ChatLogScope = "github.com/speakeasy-api/gram/server/internal/chatevents"

// ChatLog reads the records the chat message bridge logs: one per persisted
// user or assistant message, with the text in the body and identity in the
// gram.* attributes the bridge stamped. Only prompts and responses come this
// way; tool events keep the ids their own channel gave them.
type ChatLog struct{}

func (ChatLog) AppliesTo(record *otelv1.InboundLogRecord) bool {
	return record.GetScope().GetName() == ChatLogScope
}

func (ChatLog) InputContent(*otelv1.InboundLogRecord) (string, genaiconv.InputMessages, error) {
	return "", nil, nil
}

func (ChatLog) OutputContent(*otelv1.InboundLogRecord) (string, genaiconv.OutputMessages, error) {
	return "", nil, nil
}

func (ChatLog) SessionID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, string(attr.SessionIDKey))
	return key, value, nil
}

func (ChatLog) ExternalUserEmail(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, userEmailKey)
	return key, value, nil
}

func (ChatLog) ExternalUserID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, string(attr.ExternalUserIDKey))
	return key, value, nil
}

func (ChatLog) ResponseID(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

// Provider: the bridge does not know which provider served a message.
func (ChatLog) Provider(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

func (ChatLog) Surface(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, string(attr.HookSourceKey))
	return key, value, nil
}

func (ChatLog) EventName(record *otelv1.InboundLogRecord) (string, string, error) {
	key, name := logRawEventName(record)
	return key, name, nil
}

func (ChatLog) EventType(record *otelv1.InboundLogRecord) (string, string, error) {
	key, role := getOneLogAttr(record, string(attr.ChatRoleKey))
	switch role {
	case "user":
		return key, EventTypePrompt, nil
	case "assistant":
		return key, EventTypeAPIResponse, nil
	}
	return "", "", nil
}

// SubjectID is the content-derived key, so the same message captured by two
// channels lands as one event.
func (ChatLog) SubjectID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, string(attr.ChatMessageKeyKey))
	return key, value, nil
}

func (ChatLog) TurnID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, string(attr.HookTurnIDKey))
	return key, value, nil
}

func (ChatLog) Model(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, string(attr.GenAIResponseModelKey))
	return key, value, nil
}

func (ChatLog) ToolName(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

func (ChatLog) Outcome(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

func (ChatLog) OutcomeMessage(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

func (ChatLog) Text(record *otelv1.InboundLogRecord) (string, string, error) {
	if body := record.GetBody(); body.HasStringValue() && body.GetStringValue() != "" {
		return "body", body.GetStringValue(), nil
	}
	return "", "", nil
}

func (ChatLog) DurationNano(*otelv1.InboundLogRecord) (string, int64, error) {
	return "", 0, nil
}

func (ChatLog) InputTokens(*otelv1.InboundLogRecord) (string, int64, error) {
	return "", 0, nil
}

func (ChatLog) OutputTokens(*otelv1.InboundLogRecord) (string, int64, error) {
	return "", 0, nil
}

func (ChatLog) CacheReadTokens(*otelv1.InboundLogRecord) (string, int64, error) {
	return "", 0, nil
}

func (ChatLog) CacheWriteTokens(*otelv1.InboundLogRecord) (string, int64, error) {
	return "", 0, nil
}

func (ChatLog) CostUSD(*otelv1.InboundLogRecord) (string, float64, error) {
	return "", 0, nil
}

func (ChatLog) QuerySource(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

func (ChatLog) SkillName(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

func (ChatLog) AgentName(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

func (ChatLog) MCPServerName(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

func (ChatLog) MCPToolName(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

func (ChatLog) ExternalOrgID(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}
