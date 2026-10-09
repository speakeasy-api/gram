package dialect

import (
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/genaiconv"
)

const (
	// GramGatewayLogScope is the scope the MCP gateway emits tool call records under.
	GramGatewayLogScope = "github.com/speakeasy-api/gram/server/internal/mcp"

	// The two records of one tool call share gram.tool_call.id.
	GramToolCallStartedEvent   = "gram.tool_call.started"
	GramToolCallCompletedEvent = "gram.tool_call.completed"

	// nanosPerSecond converts the gateway's seconds into agent_events' nanoseconds.
	nanosPerSecond = 1e9
)

// GramLog reads the records the MCP gateway emits for the tool calls it runs.
type GramLog struct{}

func (GramLog) AppliesTo(record *otelv1.InboundLogRecord) bool {
	return record.GetScope().GetName() == GramGatewayLogScope
}

// InputContent is empty: tool IO is not a conversation.
func (GramLog) InputContent(*otelv1.InboundLogRecord) (string, genaiconv.InputMessages, error) {
	return "", nil, nil
}

func (GramLog) OutputContent(*otelv1.InboundLogRecord) (string, genaiconv.OutputMessages, error) {
	return "", nil, nil
}

// SessionID is the MCP session, else the chat, else the conversation id.
func (GramLog) SessionID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttrAny(record,
		string(attr.SessionIDKey),
		string(attr.ChatIDKey),
		string(attr.GenAIConversationIDKey),
	)
	return key, value, nil
}

func (GramLog) ExternalUserID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, string(attr.ExternalUserIDKey))
	return key, value, nil
}

func (GramLog) ExternalUserEmail(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, userEmailKey)
	return key, value, nil
}

func (GramLog) ResponseID(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

// Provider is empty: a tool call involves no model provider.
func (GramLog) Provider(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

// Surface is the MCP client that made the call, as it introduced itself.
func (GramLog) Surface(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, string(attr.McpClientNameKey))
	return key, value, nil
}

func (GramLog) EventName(record *otelv1.InboundLogRecord) (string, string, error) {
	key, name := logRawEventName(record)
	return key, name, nil
}

// EventType is tool_call for the started record and tool_call_result for the completed one.
func (GramLog) EventType(record *otelv1.InboundLogRecord) (string, string, error) {
	key, name := logRawEventName(record)
	switch name {
	case GramToolCallStartedEvent:
		return key, EventTypeToolCall, nil
	case GramToolCallCompletedEvent:
		return key, EventTypeToolCallResult, nil
	}
	return "", EventTypeUnclassified, nil
}

// SubjectID is the tool call both records belong to.
func (GramLog) SubjectID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, string(attr.ToolCallIDKey))
	return key, value, nil
}

func (GramLog) TurnID(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

func (GramLog) Model(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

func (GramLog) ToolName(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, string(attr.ToolNameKey))
	return key, value, nil
}

// Outcome is gram.outcome, else derived from the HTTP status.
func (GramLog) Outcome(record *otelv1.InboundLogRecord) (string, string, error) {
	if key, outcome := getOneLogAttr(record, string(attr.OutcomeKey)); key != "" {
		return key, outcome, nil
	}
	key, status := getOneLogInt64(record, string(attr.HTTPResponseStatusCodeKey))
	if key == "" {
		return "", "", nil
	}
	if status < 200 || status >= 300 {
		return key, OutcomeError, nil
	}
	return key, OutcomeOK, nil
}

// OutcomeMessage is the error the gateway answered the client with.
func (GramLog) OutcomeMessage(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, string(attr.ErrorMessageKey))
	return key, value, nil
}

func (GramLog) Text(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

// DurationNano is the gateway's timing of the call, recorded in seconds.
func (GramLog) DurationNano(record *otelv1.InboundLogRecord) (string, int64, error) {
	key, seconds := getOneLogFloat64(record, string(attr.ToolCallDurationKey))
	if key == "" {
		return "", 0, nil
	}
	return key, int64(seconds * nanosPerSecond), nil
}

func (GramLog) InputTokens(*otelv1.InboundLogRecord) (string, int64, error) {
	return "", 0, nil
}

func (GramLog) OutputTokens(*otelv1.InboundLogRecord) (string, int64, error) {
	return "", 0, nil
}

func (GramLog) CacheReadTokens(*otelv1.InboundLogRecord) (string, int64, error) {
	return "", 0, nil
}

func (GramLog) CacheWriteTokens(*otelv1.InboundLogRecord) (string, int64, error) {
	return "", 0, nil
}

func (GramLog) CostUSD(*otelv1.InboundLogRecord) (string, float64, error) {
	return "", 0, nil
}

func (GramLog) QuerySource(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

func (GramLog) SkillName(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

func (GramLog) AgentName(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

// MCPServerName is the hosted MCP server's toolset slug.
func (GramLog) MCPServerName(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, string(attr.ToolsetSlugKey))
	return key, value, nil
}

// MCPToolName is the tool's name: every tool the gateway runs is served over MCP.
func (GramLog) MCPToolName(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, string(attr.ToolNameKey))
	return key, value, nil
}

func (GramLog) ExternalOrgID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, string(attr.ExternalOrgIDKey))
	return key, value, nil
}
