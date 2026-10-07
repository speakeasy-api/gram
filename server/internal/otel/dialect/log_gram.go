package dialect

import (
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/genaiconv"
)

const (
	// GramGatewayLogScope is the instrumentation scope the MCP gateway emits
	// its tool call records under: the gateway's own package, as an OTel
	// instrumentation library names itself.
	GramGatewayLogScope = "github.com/speakeasy-api/gram/server/internal/mcp"

	// GramToolCallStartedEvent is emitted before the gateway runs a tool,
	// and GramToolCallCompletedEvent once the call has returned. The two
	// records of one call share gram.tool_call.id.
	GramToolCallStartedEvent   = "gram.tool_call.started"
	GramToolCallCompletedEvent = "gram.tool_call.completed"

	// nanosPerSecond converts the seconds the gateway records a duration in
	// into the nanoseconds agent_events stores.
	nanosPerSecond = 1e9
)

// GramLog reads the records the MCP gateway emits for the tool calls it
// runs. A call is two records: the started record is the tool_call, the
// completed record the tool_call_result, and both name the same subject,
// the tool call id. No model provider is involved, and the agent surface is
// whatever MCP client made the call, as it introduced itself.
type GramLog struct{}

func (GramLog) AppliesTo(record *otelv1.InboundLogRecord) bool {
	return record.GetScope().GetName() == GramGatewayLogScope
}

// InputContent and OutputContent: the tool's arguments and result are tool
// IO, not a conversation, so there are no messages to normalize.
func (GramLog) InputContent(*otelv1.InboundLogRecord) (string, genaiconv.InputMessages, error) {
	return "", nil, nil
}

func (GramLog) OutputContent(*otelv1.InboundLogRecord) (string, genaiconv.OutputMessages, error) {
	return "", nil, nil
}

// SessionID is the MCP session the call was made on, else the chat it was
// made from, else the conversation id the call carried.
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

// Provider: a tool call involves no model provider.
func (GramLog) Provider(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

// Surface is the MCP client that made the call, as it introduced itself. The
// key is the attribute, not the scope, since the surface is read from the
// record rather than known from recognising the producer.
func (GramLog) Surface(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, string(attr.McpClientNameKey))
	return key, value, nil
}

func (GramLog) EventName(record *otelv1.InboundLogRecord) (string, string, error) {
	key, name := logRawEventName(record)
	return key, name, nil
}

// EventType: the started record is the call, the completed record its
// result. Anything else on this scope is unclassified.
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

// Outcome is what the gateway said about the call under gram.outcome, which
// it derives by the rule that marks the tool result isError for the client.
// A record without that verdict falls back to the HTTP status it recorded,
// under the same rule: anything but a 2xx failed.
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

// OutcomeMessage is the message the gateway answered the client with when
// the call failed before or while running. A tool that returned its own
// error document carries none: the gateway records the result, not a
// message about it.
func (GramLog) OutcomeMessage(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, string(attr.ErrorMessageKey))
	return key, value, nil
}

func (GramLog) Text(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

// DurationNano is how long the call took by the gateway's own timing, which
// it records in seconds.
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

// MCPServerName is the hosted MCP server the tool belongs to, by its
// toolset slug. A call dispatched without one is counted.
func (GramLog) MCPServerName(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, string(attr.ToolsetSlugKey))
	return key, value, nil
}

// MCPToolName is the tool itself: every tool the gateway runs is served over
// MCP, so the tool's name is the MCP tool's name.
func (GramLog) MCPToolName(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, string(attr.ToolNameKey))
	return key, value, nil
}

func (GramLog) ExternalOrgID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, string(attr.ExternalOrgIDKey))
	return key, value, nil
}
