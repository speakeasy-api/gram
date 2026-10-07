package dialect

import (
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/genaiconv"
)

const (
	// GramTelemetryLogScope is the instrumentation scope under which Gram's
	// own telemetry rows enter the OTel pipeline: the tool calls the gateway
	// ran, republished by the tool-call log bridge and relayed to customer
	// destinations by the tool-call log relay.
	GramTelemetryLogScope = "github.com/speakeasy-api/gram/server/internal/telemetry"

	// GramToolCallEvent is the event name of a tool call Gram ran.
	GramToolCallEvent = "gram.tool_call"

	// httpServerRequestDurationKey is the duration a remote MCP tool call
	// records, in seconds, when the gateway did not time the call itself.
	httpServerRequestDurationKey = "http.server.request.duration"

	// nanosPerSecond converts the seconds Gram's producers record into the
	// nanoseconds agent_events stores.
	nanosPerSecond = 1e9
)

// GramLog reads the tool call records Gram itself writes when it runs a tool
// through the gateway. One row is the whole call, observed after it
// completed, so it is a tool_call_result: it carries the tool, the MCP
// server, how the call went and how long it took. No model provider is
// involved, and the agent surface is whatever MCP client made the call.
type GramLog struct{}

func (GramLog) AppliesTo(record *otelv1.InboundLogRecord) bool {
	return record.GetScope().GetName() == GramTelemetryLogScope
}

// InputContent and OutputContent: the tool's arguments and result are tool
// IO, not a conversation, so there are no messages to normalize.
func (GramLog) InputContent(*otelv1.InboundLogRecord) (string, genaiconv.InputMessages, error) {
	return "", nil, nil
}

func (GramLog) OutputContent(*otelv1.InboundLogRecord) (string, genaiconv.OutputMessages, error) {
	return "", nil, nil
}

// SessionID is the gateway's own session when it stamped one, else the chat
// the call was made from, else the conversation id the tool call carried.
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

// EventType: the row is written once the call completed, with its status
// and duration, so it is the result of the call. Anything else on this scope
// is unclassified.
func (GramLog) EventType(record *otelv1.InboundLogRecord) (string, string, error) {
	key, name := logRawEventName(record)
	if name == GramToolCallEvent {
		return key, EventTypeToolCallResult, nil
	}
	return "", "", nil
}

// SubjectID is the telemetry row the record was bridged from: Gram gives a
// tool call no id of its own, and the row is the one observation of it.
func (GramLog) SubjectID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, string(attr.TelemetryLogIDKey))
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

// Outcome is read off the HTTP status the gateway recorded for the call, the
// same way the telemetry writer derives the row's severity.
func (GramLog) Outcome(record *otelv1.InboundLogRecord) (string, string, error) {
	key, status := getOneLogInt64(record, string(attr.HTTPResponseStatusCodeKey))
	if key == "" {
		return "", "", nil
	}
	if status >= 400 {
		return key, OutcomeError, nil
	}
	return key, OutcomeOK, nil
}

// OutcomeMessage: the gateway records the tool's result document, not a
// message about the outcome, so none is stated.
func (GramLog) OutcomeMessage(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

func (GramLog) Text(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

// DurationNano is how long the call took: the gateway's own timing, or the
// request duration a remote MCP call recorded. Both are seconds.
func (GramLog) DurationNano(record *otelv1.InboundLogRecord) (string, int64, error) {
	key, seconds := getOneLogFloat64(record, string(attr.ToolCallDurationKey), httpServerRequestDurationKey)
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

// MCPServerName is the hosted MCP server the tool belongs to, by its slug. A
// remote or tunneled MCP call carries no slug and is counted.
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
