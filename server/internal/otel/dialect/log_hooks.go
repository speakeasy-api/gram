package dialect

import (
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/genaiconv"
	"github.com/speakeasy-api/gram/server/internal/toolref"
)

// HooksLogScopeName is the instrumentation scope under which the rows the
// hooks ingest endpoint writes enter the OTel pipeline. The hooks package
// republishes every telemetry row it writes under this scope, with the
// gram.hook.* attributes the row carries.
const HooksLogScopeName = "github.com/speakeasy-api/gram/server/internal/hooks"

// The gram.hook.event vocabulary: the provider-style hook names every hooks
// endpoint has always stored, which the hooks package owns. They are
// repeated here rather than imported because this package is a leaf.
const (
	hookEventPreToolUse         = "PreToolUse"
	hookEventPostToolUse        = "PostToolUse"
	hookEventPostToolUseFailure = "PostToolUseFailure"
	hookEventUserPromptSubmit   = "UserPromptSubmit"
	hookEventBeforeSubmitPrompt = "BeforeSubmitPrompt"
	hookEventPermissionRequest  = "PermissionRequest"
	hookEventBeforeMCPExecution = "BeforeMCPExecution"
	hookEventAfterMCPExecution  = "AfterMCPExecution"

	hookSourceKey          = "gram.hook.source"
	hookErrorKey           = "gram.hook.error"
	hookBlockReasonKey     = "gram.hook.block_reason"
	hookDecisionKey        = "gram.hook.decision"
	hookTurnIDKey          = "gram.hook.turn_id"
	hookToolNameKey        = "gram.tool.name"
	hookToolCallSourceKey  = "gram.tool_call.source"
	hookToolCallDuration   = "gram.tool_call.duration"
	hookProviderKey        = "gram.provider"
	hookExternalOrgIDKey   = "gram.external_org_id"
	hookExternalUserIDKey  = "gram.external_user.id"
	hookSessionIDKey       = "session.id"
	hookResponseModelKey   = "gen_ai.response.model"
	hookToolCallIDKey      = "gen_ai.tool.call.id"
	hookUsageInputKey      = "gen_ai.usage.input_tokens"
	hookUsageOutputKey     = "gen_ai.usage.output_tokens"
	hookUsageCacheReadKey  = "gen_ai.usage.cache_read_input_tokens"
	hookUsageCacheWriteKey = "gen_ai.usage.cache_creation_input_tokens"
	hookUsageCostKey       = "gen_ai.usage.cost"

	providerAnthropic = "anthropic"
	providerOpenAI    = "openai"
)

// HooksLog reads the rows the hooks ingest endpoint writes: one row per hook
// event an agent's hooks delivered, named by the provider-style hook event
// and carrying the tool, the session and how the call went as gram.hook.*
// attributes. The agent surface is the hook_source the endpoint resolved.
// Only the tool, prompt and permission hooks have a type; session
// lifecycle, notifications and Gram's own derived rows stay unclassified
// with their raw name kept.
type HooksLog struct{}

func (HooksLog) AppliesTo(record *otelv1.InboundLogRecord) bool {
	return record.GetScope().GetName() == HooksLogScopeName
}

// hooksEventType is the type a hook event name implies, or "" when the
// event is not one agent_events classifies.
func hooksEventType(name string) string {
	switch name {
	case hookEventPreToolUse, hookEventBeforeMCPExecution:
		return EventTypeToolCall
	case hookEventPostToolUse, hookEventPostToolUseFailure, hookEventAfterMCPExecution:
		return EventTypeToolCallResult
	case hookEventUserPromptSubmit, hookEventBeforeSubmitPrompt:
		return EventTypePrompt
	case hookEventPermissionRequest:
		return EventTypeToolDecision
	default:
		return ""
	}
}

// InputContent and OutputContent: a hook row carries the tool's arguments
// and result, not a conversation; the prompt itself goes to chat, not
// telemetry.
func (HooksLog) InputContent(*otelv1.InboundLogRecord) (string, genaiconv.InputMessages, error) {
	return "", nil, nil
}

func (HooksLog) OutputContent(*otelv1.InboundLogRecord) (string, genaiconv.OutputMessages, error) {
	return "", nil, nil
}

// SessionID is the agent's own session id, as the other dialects answer it
// for the same session; the chat it maps to rides as gen_ai.conversation.id.
func (HooksLog) SessionID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, hookSessionIDKey)
	return key, value, nil
}

// ExternalUserID is the AI account the hooks endpoint attributed the
// session to, when it had one. The row's user.id is the Gram user the
// endpoint resolved, not an account at a provider.
func (HooksLog) ExternalUserID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, hookExternalUserIDKey)
	return key, value, nil
}

func (HooksLog) ExternalUserEmail(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, userEmailKey)
	return key, value, nil
}

func (HooksLog) ResponseID(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

// Provider is the account attribution the hooks path stamped when it had
// one, else what the surface family implies: Anthropic's own surfaces and
// Codex are known, anything else is not stated.
func (HooksLog) Provider(record *otelv1.InboundLogRecord) (string, string, error) {
	if key, value := getOneLogAttr(record, hookProviderKey); key != "" {
		return key, value, nil
	}
	key, surface := getOneLogAttr(record, hookSourceKey)
	switch surface {
	case "claude-code", "claude-code-desktop", "cowork", "claude-tag":
		return key, providerAnthropic, nil
	case "codex":
		return key, providerOpenAI, nil
	default:
		return "", "", nil
	}
}

// Surface is the hook_source the endpoint resolved for the session. It is
// read off the record, so it is a column value but not a counter label.
func (HooksLog) Surface(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, hookSourceKey)
	return key, value, nil
}

func (HooksLog) EventName(record *otelv1.InboundLogRecord) (string, string, error) {
	key, name := logRawEventName(record)
	return key, name, nil
}

func (HooksLog) EventType(record *otelv1.InboundLogRecord) (string, string, error) {
	key, name := logRawEventName(record)
	if eventType := hooksEventType(name); eventType != "" {
		return key, eventType, nil
	}
	return "", "", nil
}

// SubjectID is the tool call on the tool hooks, so a call's request, result
// and decision share one event id. Codex states no per-call id, so its rows
// fall back to the record id.
func (HooksLog) SubjectID(record *otelv1.InboundLogRecord) (string, string, error) {
	_, name := logRawEventName(record)
	switch hooksEventType(name) {
	case EventTypeToolCall, EventTypeToolCallResult, EventTypeToolDecision:
		key, value := getOneLogAttr(record, hookToolCallIDKey)
		return key, value, nil
	default:
		return "", "", nil
	}
}

func (HooksLog) TurnID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, hookTurnIDKey)
	return key, value, nil
}

func (HooksLog) Model(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, hookResponseModelKey)
	return key, value, nil
}

func (HooksLog) ToolName(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, hookToolNameKey)
	return key, value, nil
}

// Outcome is what the hook event says about the call: a completed tool ran,
// a failure did not, an MCP call that reported an error failed, and a
// permission request Gram denied was rejected. A pre-call hook and a prompt
// have no outcome.
func (HooksLog) Outcome(record *otelv1.InboundLogRecord) (string, string, error) {
	nameKey, name := logRawEventName(record)
	switch name {
	case hookEventPostToolUse:
		return nameKey, OutcomeOK, nil
	case hookEventPostToolUseFailure:
		return nameKey, OutcomeError, nil
	case hookEventAfterMCPExecution:
		if key, _ := getOneLogAttr(record, hookErrorKey); key != "" {
			return key, OutcomeError, nil
		}
		return nameKey, OutcomeOK, nil
	case hookEventPermissionRequest:
		if key, _ := getOneLogAttr(record, hookBlockReasonKey); key != "" {
			return key, OutcomeRejected, nil
		}
		return "", "", nil
	default:
		return "", "", nil
	}
}

// OutcomeMessage is the error a tool result reported, when it did.
func (HooksLog) OutcomeMessage(record *otelv1.InboundLogRecord) (string, string, error) {
	_, name := logRawEventName(record)
	if hooksEventType(name) != EventTypeToolCallResult {
		return "", "", nil
	}
	key, value := getOneLogAttr(record, hookErrorKey)
	return key, value, nil
}

// Text on a permission request is Gram's verdict; the prompt's words go to
// chat, not telemetry, so a prompt has no text here.
func (HooksLog) Text(record *otelv1.InboundLogRecord) (string, string, error) {
	_, name := logRawEventName(record)
	if hooksEventType(name) != EventTypeToolDecision {
		return "", "", nil
	}
	key, value := getOneLogAttr(record, hookDecisionKey)
	return key, value, nil
}

// DurationNano is how long the tool ran, which the hooks endpoint records in
// seconds.
func (HooksLog) DurationNano(record *otelv1.InboundLogRecord) (string, int64, error) {
	key, seconds := getOneLogFloat64(record, hookToolCallDuration)
	if key == "" {
		return "", 0, nil
	}
	return key, int64(seconds * nanosPerSecond), nil
}

func (HooksLog) InputTokens(record *otelv1.InboundLogRecord) (string, int64, error) {
	key, value := getOneLogInt64(record, hookUsageInputKey)
	return key, value, nil
}

func (HooksLog) OutputTokens(record *otelv1.InboundLogRecord) (string, int64, error) {
	key, value := getOneLogInt64(record, hookUsageOutputKey)
	return key, value, nil
}

func (HooksLog) CacheReadTokens(record *otelv1.InboundLogRecord) (string, int64, error) {
	key, value := getOneLogInt64(record, hookUsageCacheReadKey)
	return key, value, nil
}

func (HooksLog) CacheWriteTokens(record *otelv1.InboundLogRecord) (string, int64, error) {
	key, value := getOneLogInt64(record, hookUsageCacheWriteKey)
	return key, value, nil
}

func (HooksLog) CostUSD(record *otelv1.InboundLogRecord) (string, float64, error) {
	key, value := getOneLogFloat64(record, hookUsageCostKey)
	return key, value, nil
}

func (HooksLog) QuerySource(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

func (HooksLog) SkillName(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

func (HooksLog) AgentName(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

// MCPServerName is the MCP server the hooks endpoint attributed the call to.
func (HooksLog) MCPServerName(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, hookToolCallSourceKey)
	return key, value, nil
}

// MCPToolName is the function behind a Claude-style mcp__server__function
// tool name, or the tool itself when the row names an MCP server for a tool
// that is not prefixed, as Cursor's MCP hooks do.
func (HooksLog) MCPToolName(record *otelv1.InboundLogRecord) (string, string, error) {
	key, name := getOneLogAttr(record, hookToolNameKey)
	if key == "" {
		return "", "", nil
	}
	if toolref.IsMCPToolName(name) {
		return key, toolref.MCPFunctionOf(name), nil
	}
	if serverKey, _ := getOneLogAttr(record, hookToolCallSourceKey); serverKey != "" {
		return key, name, nil
	}
	return "", "", nil
}

func (HooksLog) ExternalOrgID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, hookExternalOrgIDKey)
	return key, value, nil
}
