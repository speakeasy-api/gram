package dialect

import (
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/toolref"
)

// HooksLogScope is the scope the hooks ingest endpoint republishes its telemetry rows under.
const HooksLogScope = "github.com/speakeasy-api/gram/server/internal/hooks"

// The hook event names the hooks package stores, repeated here because this
// package is a leaf.
const (
	hookEventPreToolUse         = "PreToolUse"
	hookEventPostToolUse        = "PostToolUse"
	hookEventPostToolUseFailure = "PostToolUseFailure"
	hookEventUserPromptSubmit   = "UserPromptSubmit"
	hookEventBeforeSubmitPrompt = "BeforeSubmitPrompt"
	hookEventPermissionRequest  = "PermissionRequest"
	hookEventBeforeMCPExecution = "BeforeMCPExecution"
	hookEventAfterMCPExecution  = "AfterMCPExecution"
)

const (
	hookSourceKey         = "gram.hook.source"
	hookErrorKey          = "gram.hook.error"
	hookBlockReasonKey    = "gram.hook.block_reason"
	hookDecisionKey       = "gram.hook.decision"
	hookTurnIDKey         = "gram.hook.turn_id"
	hookToolNameKey       = "gram.tool.name"
	hookToolCallSourceKey = "gram.tool_call.source"
	hookToolCallDuration  = "gram.tool_call.duration"
	hookExternalOrgIDKey  = "gram.external_org_id"
	hookExternalUserIDKey = "gram.external_user.id"
	hookSessionIDKey      = "gram.session.id"
	hookRawSessionIDKey   = "session.id"
	hookToolCallIDKey     = "gen_ai.tool.call.id"
)

// HookLog reads the rows the hooks ingest endpoint republishes: one record
// per hook event, named by the provider-style hook event. Only the tool,
// prompt and permission hooks have a type; session lifecycle,
// notifications and derived rows stay unclassified with their raw name kept.
type HookLog struct {
	SemconvLog
}

func (HookLog) AppliesTo(record *otelv1.InboundLogRecord) bool {
	return record.GetScope().GetName() == HooksLogScope
}

// hookEventType is the type a hook event name implies, or "" when the event
// is not one agent_events classifies.
func hookEventType(name string) string {
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
		return EventTypeUnclassified
	}
}

func (HookLog) EventType(record *otelv1.InboundLogRecord) (string, string, error) {
	key, name := logRawEventName(record)
	if eventType := hookEventType(name); eventType != EventTypeUnclassified {
		return key, eventType, nil
	}
	return "", "", nil
}

// Provider is what the hook source's family implies. A provider the
// pipeline attributed (gram.provider) still wins over it.
func (HookLog) Provider(record *otelv1.InboundLogRecord) (string, string, error) {
	key, source := getOneLogAttr(record, hookSourceKey)
	switch source {
	case "claude-code", "claude-code-desktop", "cowork", "claude-tag":
		return key, "anthropic", nil
	case "codex":
		return key, "openai", nil
	default:
		return "", "", nil
	}
}

// Surface is the hook source the endpoint resolved.
func (HookLog) Surface(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, hookSourceKey)
	return key, value, nil
}

// SessionID is the agent's own session id, not the chat it maps to under
// gen_ai.conversation.id.
func (HookLog) SessionID(record *otelv1.InboundLogRecord) (string, string, error) {
	if key, value := getOneLogAttr(record, hookSessionIDKey); key != "" {
		return key, value, nil
	}
	key, value := getOneLogAttr(record, hookRawSessionIDKey)
	return key, value, nil
}

func (HookLog) TurnID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, hookTurnIDKey)
	return key, value, nil
}

// SubjectID is the tool call on the tool hooks, so a call's request, result
// and decision share one event id.
func (HookLog) SubjectID(record *otelv1.InboundLogRecord) (string, string, error) {
	_, name := logRawEventName(record)
	switch hookEventType(name) {
	case EventTypeToolCall, EventTypeToolCallResult, EventTypeToolDecision:
		key, value := getOneLogAttr(record, hookToolCallIDKey)
		return key, value, nil
	default:
		return "", "", nil
	}
}

// ExternalUserID is the AI account the endpoint attributed the session to.
// The record's user.id is the Speakeasy user, never an external account.
func (HookLog) ExternalUserID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, hookExternalUserIDKey)
	return key, value, nil
}

func (HookLog) ExternalOrgID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, hookExternalOrgIDKey)
	return key, value, nil
}

func (HookLog) ToolName(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, hookToolNameKey)
	return key, value, nil
}

func (HookLog) MCPServerName(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, hookToolCallSourceKey)
	return key, value, nil
}

// MCPToolName is the function behind a Claude-style mcp__server__function
// tool name, or the tool itself when the row names an MCP server for an
// unprefixed tool, as Cursor's MCP hooks do.
func (HookLog) MCPToolName(record *otelv1.InboundLogRecord) (string, string, error) {
	key, name := getOneLogAttr(record, hookToolNameKey)
	switch {
	case key == "":
		return "", "", nil
	case toolref.IsMCPToolName(name):
		return key, toolref.MCPFunctionOf(name), nil
	}
	if serverKey, _ := getOneLogAttr(record, hookToolCallSourceKey); serverKey == "" {
		return "", "", nil
	}
	return key, name, nil
}

// Outcome is what the hook event says about the call: a failure did not
// run, a result carrying an error failed, and a permission request
// Speakeasy blocked was rejected.
func (HookLog) Outcome(record *otelv1.InboundLogRecord) (string, string, error) {
	nameKey, name := logRawEventName(record)
	switch name {
	case hookEventPostToolUseFailure:
		return nameKey, OutcomeError, nil
	case hookEventPostToolUse, hookEventAfterMCPExecution:
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

func (HookLog) OutcomeMessage(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, hookErrorKey)
	return key, value, nil
}

// Text on a permission request is Speakeasy's verdict; a prompt's words go
// to chat, not telemetry.
func (HookLog) Text(record *otelv1.InboundLogRecord) (string, string, error) {
	_, name := logRawEventName(record)
	if hookEventType(name) != EventTypeToolDecision {
		return "", "", nil
	}
	key, value := getOneLogAttr(record, hookDecisionKey)
	return key, value, nil
}

// DurationNano is how long the tool ran, which the endpoint records in seconds.
func (HookLog) DurationNano(record *otelv1.InboundLogRecord) (string, int64, error) {
	key, seconds := getOneLogFloat64(record, hookToolCallDuration)
	if key == "" {
		return "", 0, nil
	}
	return key, int64(seconds * 1e9), nil
}
