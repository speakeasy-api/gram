package dialect

import (
	"encoding/json"
	"strings"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/agentsurface"
	"github.com/speakeasy-api/gram/server/internal/genaiconv"
)

// HookLogScopeName is the instrumentation scope a hook event record carries.
// No producer sends it: whatever lifts the hook events the hooks endpoints
// record into the pipeline stamps it, so HookLog claims them.
const HookLogScopeName = "github.com/speakeasy-api/gram/server/internal/hooks"

// The attributes the hooks endpoints stamp on a hook event row. Gram writes
// them, after authenticating the hook request, so they are one vocabulary
// across Claude, Codex, Cursor and the unified ingest endpoint.
const (
	hookSourceKey         = "gram.hook.source"
	hookBlockReasonKey    = "gram.hook.block_reason"
	hookErrorKey          = "gram.hook.error"
	hookProviderKey       = "gram.provider"
	hookExternalOrgIDKey  = "gram.external_org_id"
	hookExternalUserIDKey = "gram.external_user.id"
	hookToolNameKey       = "gram.tool.name"
	hookToolSourceKey     = "gram.tool_call.source"
	hookToolDurationKey   = "gram.tool_call.duration"
	hookConversationIDKey = "gen_ai.conversation.id"
	hookToolCallIDKey     = "gen_ai.tool.call.id"
	hookToolArgumentsKey  = "gen_ai.tool.call.arguments"
	hookModelKey          = "gen_ai.response.model"

	// hookSkillToolName is the tool name every hook writer uses for a skill
	// activation, whose arguments carry {"skill": "<name>"}.
	hookSkillToolName = "Skill"
)

// The hook event names that classify, case-folded. The legacy provider
// endpoints store the provider's own name, and the unified ingest endpoint
// stores the same names for the canonical events it translates, so one
// table serves every writer. Anything else (session start and end,
// notifications, thoughts, config changes) stays unclassified but named.
var hookEventTypes = map[string]string{
	"pretooluse":         EventTypeToolCall,
	"beforemcpexecution": EventTypeToolCall,
	"permissionrequest":  EventTypeToolCall,
	"posttooluse":        EventTypeToolCallResult,
	"aftermcpexecution":  EventTypeToolCallResult,
	"posttoolusefailure": EventTypeToolCallResult,
	"userpromptsubmit":   EventTypePrompt,
	"beforesubmitprompt": EventTypePrompt,
	"afteragentresponse": EventTypeAPIResponse,
}

// hookFailureEvent is the one hook event that reports a failed tool call by
// its name rather than by an error attribute.
const hookFailureEvent = "posttoolusefailure"

// HookLog reads the hook events the hooks endpoints record.
//
// It is never paired with SemconvLog. Every attribute on a hook row was
// written by Gram, so HookLog knows what each means; the generic reader would
// instead count the token usage some hooks report as if it were the model's
// own record of the request, beside the provider stream that already carries
// it. Usage and cost are therefore never stated here.
type HookLog struct{}

func (HookLog) AppliesTo(record *otelv1.InboundLogRecord) bool {
	return record.GetScope().GetName() == HookLogScopeName
}

// hookEventType classifies a hook event. A blocked tool call never ran, so
// its only observation is the decision that blocked it.
func hookEventType(record *otelv1.InboundLogRecord) (string, string) {
	key, name := logRawEventName(record)
	kind, ok := hookEventTypes[strings.ToLower(name)]
	if !ok {
		return "", EventTypeUnclassified
	}
	if kind == EventTypeToolCall {
		if blockKey, reason := getOneLogAttr(record, hookBlockReasonKey); reason != "" {
			return blockKey, EventTypeToolDecision
		}
	}
	return key, kind
}

func (HookLog) InputContent(*otelv1.InboundLogRecord) (string, genaiconv.InputMessages, error) {
	return "", nil, nil
}

func (HookLog) OutputContent(*otelv1.InboundLogRecord) (string, genaiconv.OutputMessages, error) {
	return "", nil, nil
}

// SessionID is the agent session. The unified ingest endpoint stores the
// session's chat id, which is the session id itself for the agents whose
// session ids are UUIDs (Claude, Codex, Cursor), so hook rows and the
// provider's own telemetry land in the same session.
func (HookLog) SessionID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, hookConversationIDKey)
	return key, value, nil
}

// ExternalUserEmail is the email the hooks endpoint resolved the session to.
func (HookLog) ExternalUserEmail(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, userEmailKey)
	return key, value, nil
}

func (HookLog) ExternalUserID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, hookExternalUserIDKey)
	return key, value, nil
}

func (HookLog) ResponseID(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

func (HookLog) Provider(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, hookProviderKey)
	return key, value, nil
}

// Surface is the hook source in the separator-folded spelling the other
// dialects use (claude-code, codex, cursor). A bare "claude" stays as it is:
// the legacy Claude endpoint stamps it for several products, and naming one
// would invent attribution.
func (HookLog) Surface(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, hookSourceKey)
	if key == "" {
		return "", "", nil
	}
	return key, agentsurface.Normalize(value), nil
}

func (HookLog) EventName(record *otelv1.InboundLogRecord) (string, string, error) {
	key, name := logRawEventName(record)
	return key, name, nil
}

func (HookLog) EventType(record *otelv1.InboundLogRecord) (string, string, error) {
	key, kind := hookEventType(record)
	return key, kind, nil
}

// SubjectID is the tool call's own id, the same one the provider's telemetry
// names (Claude Code's tool_use_id), so a call observed by both collapses to
// one call.
func (HookLog) SubjectID(record *otelv1.InboundLogRecord) (string, string, error) {
	switch _, kind := hookEventType(record); kind {
	case EventTypeToolCall, EventTypeToolCallResult, EventTypeToolDecision:
		key, value := getOneLogAttr(record, hookToolCallIDKey)
		return key, value, nil
	}
	return "", "", nil
}

// TurnID: no hook writer records a turn id.
func (HookLog) TurnID(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

func (HookLog) Model(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, hookModelKey)
	return key, value, nil
}

func (HookLog) ToolName(record *otelv1.InboundLogRecord) (string, string, error) {
	if _, kind := hookEventType(record); !isHookToolEvent(kind) {
		return "", "", nil
	}
	key, value := getOneLogAttr(record, hookToolNameKey)
	return key, value, nil
}

// Outcome: a blocked call or prompt was rejected by policy, a result is an
// error when the event says the call failed or carries an error, and
// otherwise ok.
func (HookLog) Outcome(record *otelv1.InboundLogRecord) (string, string, error) {
	key, kind := hookEventType(record)
	switch kind {
	case EventTypeToolDecision:
		return key, OutcomeRejected, nil
	case EventTypePrompt:
		if blockKey, reason := getOneLogAttr(record, hookBlockReasonKey); reason != "" {
			return blockKey, OutcomeRejected, nil
		}
	case EventTypeToolCallResult:
		if nameKey, name := logRawEventName(record); strings.EqualFold(name, hookFailureEvent) {
			return nameKey, OutcomeError, nil
		}
		if errorKey := hookErrorAttr(record); errorKey != "" {
			return errorKey, OutcomeError, nil
		}
		return key, OutcomeOK, nil
	}
	return "", "", nil
}

func (HookLog) OutcomeMessage(record *otelv1.InboundLogRecord) (string, string, error) {
	if key, reason := getOneLogAttr(record, hookBlockReasonKey); reason != "" {
		return key, reason, nil
	}
	if _, kind := hookEventType(record); kind == EventTypeToolCallResult {
		key, message := getOneLogAttrAny(record, hookErrorKey)
		return key, message, nil
	}
	return "", "", nil
}

// Text: hook rows carry no prompt or response text; the hooks endpoints keep
// that out of the telemetry ledger.
func (HookLog) Text(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

// DurationNano is the tool call duration the unified ingest endpoint records,
// in seconds.
func (HookLog) DurationNano(record *otelv1.InboundLogRecord) (string, int64, error) {
	key, seconds := getOneLogFloat64(record, hookToolDurationKey)
	if key == "" || seconds < 0 {
		return "", 0, nil
	}
	return key, int64(seconds * 1e9), nil
}

func (HookLog) InputTokens(*otelv1.InboundLogRecord) (string, int64, error) {
	return "", 0, nil
}

func (HookLog) OutputTokens(*otelv1.InboundLogRecord) (string, int64, error) {
	return "", 0, nil
}

func (HookLog) CacheReadTokens(*otelv1.InboundLogRecord) (string, int64, error) {
	return "", 0, nil
}

func (HookLog) CacheWriteTokens(*otelv1.InboundLogRecord) (string, int64, error) {
	return "", 0, nil
}

func (HookLog) CostUSD(*otelv1.InboundLogRecord) (string, float64, error) {
	return "", 0, nil
}

func (HookLog) QuerySource(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

// SkillName is the skill a Skill tool call activated, from its arguments.
func (HookLog) SkillName(record *otelv1.InboundLogRecord) (string, string, error) {
	if _, name := getOneLogAttr(record, hookToolNameKey); name != hookSkillToolName {
		return "", "", nil
	}
	key, raw := getOneLogAttr(record, hookToolArgumentsKey)
	if key == "" {
		return "", "", nil
	}
	var arguments struct {
		Skill string `json:"skill"`
	}
	if err := json.Unmarshal([]byte(raw), &arguments); err != nil || strings.TrimSpace(arguments.Skill) == "" {
		return "", "", nil
	}
	return key, strings.TrimSpace(arguments.Skill), nil
}

func (HookLog) AgentName(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

// MCPServerName: the hooks endpoints set the tool call source only for MCP
// tools, naming the server.
func (HookLog) MCPServerName(record *otelv1.InboundLogRecord) (string, string, error) {
	if _, kind := hookEventType(record); !isHookToolEvent(kind) {
		return "", "", nil
	}
	key, value := getOneLogAttr(record, hookToolSourceKey)
	return key, value, nil
}

// MCPToolName is the tool name when the call went to an MCP server, which the
// hooks endpoints store with the server prefix already split off.
func (HookLog) MCPToolName(record *otelv1.InboundLogRecord) (string, string, error) {
	if _, kind := hookEventType(record); !isHookToolEvent(kind) {
		return "", "", nil
	}
	if key, _ := getOneLogAttr(record, hookToolSourceKey); key == "" {
		return "", "", nil
	}
	key, value := getOneLogAttr(record, hookToolNameKey)
	return key, value, nil
}

func (HookLog) ExternalOrgID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, hookExternalOrgIDKey)
	return key, value, nil
}

func isHookToolEvent(kind string) bool {
	return kind == EventTypeToolCall || kind == EventTypeToolCallResult || kind == EventTypeToolDecision
}

// hookErrorAttr reports the key of a hook error attribute that says
// something: the writers store whatever the agent sent, a string or a
// structured value, and an empty one is no error.
func hookErrorAttr(record *otelv1.InboundLogRecord) string {
	for _, kv := range record.GetAttributes() {
		if kv.GetKey() != hookErrorKey {
			continue
		}
		value := kv.GetValue()
		switch {
		case value.HasStringValue():
			if strings.TrimSpace(value.GetStringValue()) != "" {
				return hookErrorKey
			}
		case value.HasKvlistValue(), value.HasArrayValue():
			return hookErrorKey
		}
	}
	return ""
}
