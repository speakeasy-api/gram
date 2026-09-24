package dialect

import (
	"encoding/json"
	"strings"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/genaiconv"
)

type ClaudeCodeLog struct{}

// Claude Code names every instrumentation scope it owns under one prefix: the
// meter is com.anthropic.claude_code, the event logger
// com.anthropic.claude_code.events (observed from a 2.1 CLI), and tracing
// adds its own suffix. Matching the prefix recognises all of them, and
// whatever the CLI adds next, as Claude Code.
const claudeCodeScopePrefix = "com.anthropic.claude_code"

func isClaudeCodeScope(name string) bool {
	// The root itself, or a dot-delimited descendant of it. A bare prefix test
	// would also claim a scope that merely starts with the same letters, such
	// as com.anthropic.claude_code_vendor, which is somebody else's.
	return name == claudeCodeScopePrefix ||
		strings.HasPrefix(name, claudeCodeScopePrefix+".")
}

func (ClaudeCodeLog) AppliesTo(record *otelv1.InboundLogRecord) bool {
	return isClaudeCodeScope(record.GetScope().GetName())
}

func (ClaudeCodeLog) InputContent(record *otelv1.InboundLogRecord) (string, genaiconv.InputMessages, error) {
	key, value := claudeCodeContent(record, claudeCodePromptKey, claudeCodeUserPromptKey)
	if key == "" || value == "" {
		return "", nil, nil
	}

	return key, genaiconv.InputMessages{{
		Role: genaiconv.RoleUser,
		Parts: []genaiconv.Part{&genaiconv.TextPart{
			Type:    genaiconv.PartTypeText,
			Content: value,
		}},
		Name: nil,
	}}, nil
}

// OutputContent is the assistant response as one message. Claude Code does
// not say why the model stopped, so the finish reason is left unstated.
func (ClaudeCodeLog) OutputContent(record *otelv1.InboundLogRecord) (string, genaiconv.OutputMessages, error) {
	_, name := claudeCodeEventName(record)
	if claudeCodeEventType(name) != EventTypeAPIResponse {
		return "", nil, nil
	}
	key, value := claudeCodeContent(record, "response")
	if key == "" || value == "" {
		return "", nil, nil
	}

	return key, genaiconv.OutputMessages{{
		Role: genaiconv.RoleAssistant,
		Parts: []genaiconv.Part{&genaiconv.TextPart{
			Type:    genaiconv.PartTypeText,
			Content: value,
		}},
		FinishReason: "",
		Name:         nil,
	}}, nil
}

func (ClaudeCodeLog) SessionID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, "session.id")
	return key, value, nil
}

func (ClaudeCodeLog) ExternalUserEmail(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, userEmailKey)
	return key, value, nil
}

func (ClaudeCodeLog) ExternalUserID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, vendorUserAccountIDKey)
	if key == "" {
		key, value = getOneLogAttr(record, claudeCodeAccountUUIDKey)
	}
	return key, value, nil
}

func (ClaudeCodeLog) ResponseID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, "gen_ai.response.id")
	return key, value, nil
}

const (
	// claudeCodeRedacted stands in for content Claude Code was not told to
	// log (OTEL_LOG_USER_PROMPTS, OTEL_LOG_ASSISTANT_RESPONSES,
	// OTEL_LOG_TOOL_DETAILS). The producer stated the attribute but withheld
	// its value, so a reader treats it as absent rather than as words.
	claudeCodeRedacted         = "<REDACTED>"
	claudeCodePromptKey        = "prompt"
	claudeCodeProvider         = "anthropic"
	claudeCodeSurface          = "claude-code"
	claudeCodeAccountUUIDKey   = "user.account_uuid"
	claudeCodeLegacyBodyPrefix = "claude_code."
	claudeCodeToolParamsKey    = "tool_parameters"
)

// Producer knowledge: the scope name identified Claude Code, so it is the key.

func (ClaudeCodeLog) Provider(*otelv1.InboundLogRecord) (string, string, error) {
	return scopeNameKey, claudeCodeProvider, nil
}

func (ClaudeCodeLog) Surface(*otelv1.InboundLogRecord) (string, string, error) {
	return scopeNameKey, claudeCodeSurface, nil
}

// claudeCodeEventName is the producer's own name for the event: the OTLP
// event name, the event.name attribute, or for older CLIs claude_code.<name>
// in the log body with no event name at all.
func claudeCodeEventName(record *otelv1.InboundLogRecord) (string, string) {
	if key, name := logRawEventName(record); key != "" {
		return key, name
	}
	if body := record.GetBody(); body.HasStringValue() && strings.HasPrefix(body.GetStringValue(), claudeCodeLegacyBodyPrefix) {
		return "body", body.GetStringValue()
	}
	return "", ""
}

// claudeCodeEventType maps Claude Code's documented event names onto the
// agent vocabulary. Anything else is unclassified.
func claudeCodeEventType(name string) string {
	switch strings.TrimPrefix(name, claudeCodeLegacyBodyPrefix) {
	case "user_prompt":
		return EventTypePrompt
	case "assistant_response":
		return EventTypeAPIResponse
	case "api_request":
		return EventTypeAPIRequest
	case "api_error":
		return EventTypeAPIError
	case "api_refusal":
		return EventTypeAPIRefusal
	case "tool_result":
		return EventTypeToolCallResult
	case "tool_decision":
		return EventTypeToolDecision
	case "api_request_body":
		return EventTypeAPIRequestBody
	case "api_response_body":
		return EventTypeAPIResponseBody
	case "compaction":
		return EventTypeCompaction
	}
	return EventTypeUnclassified
}

func (ClaudeCodeLog) EventName(record *otelv1.InboundLogRecord) (string, string, error) {
	key, name := claudeCodeEventName(record)
	return key, name, nil
}

func (ClaudeCodeLog) EventType(record *otelv1.InboundLogRecord) (string, string, error) {
	key, name := claudeCodeEventName(record)
	if kind := claudeCodeEventType(name); kind != EventTypeUnclassified {
		return key, kind, nil
	}
	return "", "", nil
}

// SubjectID is the natural identity of what the event is about: the
// transcript message for prompts and responses, the API request for
// requests and their errors, the tool invocation for tool events.
func (ClaudeCodeLog) SubjectID(record *otelv1.InboundLogRecord) (string, string, error) {
	_, name := claudeCodeEventName(record)
	switch claudeCodeEventType(name) {
	case EventTypePrompt, EventTypeAPIResponse:
		key, value := getOneLogAttr(record, "message.uuid")
		return key, value, nil
	case EventTypeAPIRequest, EventTypeAPIError, EventTypeAPIRefusal:
		key, value := getOneLogAttrAny(record, "request_id", "client_request_id")
		return key, value, nil
	case EventTypeAPIResponseBody:
		// A response body names the request it answers, so it lands beside
		// that request. A request body is given no id of its own, and a
		// compaction is not about anything but itself, so both keep the
		// record id they were delivered under.
		key, value := getOneLogAttr(record, "request_id")
		return key, value, nil
	case EventTypeToolCallResult, EventTypeToolDecision:
		key, value := getOneLogAttr(record, "tool_use_id")
		return key, value, nil
	}
	return "", "", nil
}

func (ClaudeCodeLog) TurnID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, "prompt.id")
	return key, value, nil
}

func (ClaudeCodeLog) Model(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, "model")
	return key, value, nil
}

func (ClaudeCodeLog) ToolName(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, "tool_name")
	return key, value, nil
}

// Outcome is implied by the event for a response, an error and a refusal,
// stated by success on a tool result and on a compaction, and rejected when a
// tool decision said no. A request is none of these: it records that a call
// was made, not how it went, so it carries no outcome and must not be given
// one. An accepted decision has no outcome of its own either: the result row
// carries it, and neither does a captured payload: the request it belongs to
// does.
func (ClaudeCodeLog) Outcome(record *otelv1.InboundLogRecord) (string, string, error) {
	key, name := claudeCodeEventName(record)
	switch claudeCodeEventType(name) {
	case EventTypeAPIResponse:
		return key, OutcomeOK, nil
	case EventTypeAPIError:
		return key, OutcomeError, nil
	case EventTypeAPIRefusal:
		return key, OutcomeRefused, nil
	case EventTypeToolCallResult, EventTypeCompaction:
		successKey, success, present := getOneLogBool(record, "success")
		if !present {
			return "", "", nil
		}
		if success {
			return successKey, OutcomeOK, nil
		}
		return successKey, OutcomeError, nil
	case EventTypeToolDecision:
		decisionKey, decision := getOneLogAttrAny(record, "decision_type", "decision")
		if decision == "reject" {
			return decisionKey, OutcomeRejected, nil
		}
	}
	return "", "", nil
}

func (ClaudeCodeLog) OutcomeMessage(record *otelv1.InboundLogRecord) (string, string, error) {
	_, name := claudeCodeEventName(record)
	switch claudeCodeEventType(name) {
	case EventTypeAPIError, EventTypeCompaction:
		key, value := claudeCodeContent(record, "error")
		return key, value, nil
	case EventTypeToolCallResult:
		// The full error is gated behind tool-detail logging; the category
		// is not.
		key, value := claudeCodeContent(record, "error", "error_type")
		return key, value, nil
	}
	return "", "", nil
}

// Text is the record in words: the prompt, the response, or the error,
// each present only when the producer chose to log it.
func (ClaudeCodeLog) Text(record *otelv1.InboundLogRecord) (string, string, error) {
	_, name := claudeCodeEventName(record)
	switch claudeCodeEventType(name) {
	case EventTypePrompt:
		// prompt is the documented attribute; user_prompt is what older
		// exporters sent.
		key, value := claudeCodeContent(record, claudeCodePromptKey, claudeCodeUserPromptKey)
		return key, value, nil
	case EventTypeAPIResponse:
		key, value := claudeCodeContent(record, "response")
		return key, value, nil
	case EventTypeAPIError:
		key, value := claudeCodeContent(record, "error")
		return key, value, nil
	case EventTypeToolDecision:
		key, value := getOneLogAttrAny(record, "decision_type", "decision")
		return key, value, nil
	}
	return "", "", nil
}

func (ClaudeCodeLog) DurationNano(record *otelv1.InboundLogRecord) (string, int64, error) {
	key, ms := getOneLogFloat64(record, "duration_ms")
	if key == "" {
		return "", 0, nil
	}
	return key, int64(ms * 1e6), nil
}

func (ClaudeCodeLog) InputTokens(record *otelv1.InboundLogRecord) (string, int64, error) {
	key, value := getOneLogInt64(record, "input_tokens")
	return key, value, nil
}

func (ClaudeCodeLog) OutputTokens(record *otelv1.InboundLogRecord) (string, int64, error) {
	key, value := getOneLogInt64(record, "output_tokens")
	return key, value, nil
}

func (ClaudeCodeLog) CacheReadTokens(record *otelv1.InboundLogRecord) (string, int64, error) {
	key, value := getOneLogInt64(record, "cache_read_tokens")
	return key, value, nil
}

func (ClaudeCodeLog) CacheWriteTokens(record *otelv1.InboundLogRecord) (string, int64, error) {
	key, value := getOneLogInt64(record, "cache_creation_tokens")
	return key, value, nil
}

// CostUSD reads dollars when stated, else millionths of a dollar.
func (ClaudeCodeLog) CostUSD(record *otelv1.InboundLogRecord) (string, float64, error) {
	if key, value := getOneLogFloat64(record, "cost_usd"); key != "" {
		return key, value, nil
	}
	key, micros := getOneLogFloat64(record, "cost_usd_micros")
	if key == "" {
		return "", 0, nil
	}
	return key, micros / 1e6, nil
}

func (ClaudeCodeLog) QuerySource(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, "query_source")
	return key, value, nil
}

func (ClaudeCodeLog) SkillName(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := claudeCodeAttribution(record, "skill.name", "skill_name")
	return key, value, nil
}

func (ClaudeCodeLog) AgentName(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := claudeCodeAttribution(record, "agent.name", "subagent_type")
	return key, value, nil
}

func (ClaudeCodeLog) MCPServerName(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := claudeCodeAttribution(record, "mcp_server.name", "mcp_server_name")
	return key, value, nil
}

func (ClaudeCodeLog) MCPToolName(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := claudeCodeAttribution(record, "mcp_tool.name", "mcp_tool_name")
	return key, value, nil
}

func (ClaudeCodeLog) ExternalOrgID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, "organization.id")
	return key, value, nil
}

// claudeCodeAttribution reads skill, agent and MCP attribution wherever
// Claude Code puts it: a dotted attribute on API events, a flat attribute
// on tool decisions, or inside the tool_parameters JSON on tool results
// when tool details are logged.
func claudeCodeAttribution(record *otelv1.InboundLogRecord, apiKey, toolKey string) (string, string) {
	if key, value := getOneLogAttr(record, apiKey); key != "" {
		return key, value
	}
	if key, value := getOneLogAttr(record, toolKey); key != "" {
		return key, value
	}
	_, params := getOneLogAttr(record, claudeCodeToolParamsKey)
	if params == "" {
		return "", ""
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(params), &decoded); err != nil {
		return "", ""
	}
	if value, ok := decoded[toolKey].(string); ok && value != "" {
		return claudeCodeToolParamsKey + "." + toolKey, value
	}
	return "", ""
}

// claudeCodeContent reads the first of keys that carries content, treating
// Claude Code's redaction sentinel as nothing stated at all.
func claudeCodeContent(record *otelv1.InboundLogRecord, keys ...string) (string, string) {
	// Per key rather than one lookup across all of them: a redacted value on an
	// earlier key says nothing, and the later keys may still carry something.
	// Stopping at the first key present would lose, say, an error_type sitting
	// behind a redacted error.
	for _, candidate := range keys {
		key, value := getOneLogAttrAny(record, candidate)
		if key == "" || value == "" || value == claudeCodeRedacted {
			continue
		}
		return key, value
	}
	return "", ""
}

// BodyRepeatsEventName reports whether a log body is only the event's name
// again, in any spelling a producer uses: bare, or under Claude Code's legacy
// claude_code. prefix. Such a body says nothing a row does not already carry.
func BodyRepeatsEventName(body, name string) bool {
	// Either side may carry the prefix, and they need not agree: a record can
	// name itself claude_code.api_request and repeat the bare api_request in
	// its body. Strip the name down first, then accept the body in either
	// spelling.
	bare := strings.TrimPrefix(name, claudeCodeLegacyBodyPrefix)
	return body == bare || body == claudeCodeLegacyBodyPrefix+bare
}
