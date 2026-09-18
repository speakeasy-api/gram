package dialect

import (
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/genaiconv"
)

const (
	codexLogScopeName       = "codex_otel.log_only"
	codexUserPromptEvent    = "codex.user_prompt"
	codexRedactedUserPrompt = "[REDACTED]"
)

type CodexLog struct{}

func (CodexLog) AppliesTo(record *otelv1.InboundLogRecord) bool {
	return record.GetScope().GetName() == codexLogScopeName
}

func (CodexLog) InputContent(record *otelv1.InboundLogRecord) (string, genaiconv.InputMessages, error) {
	switch record.GetEventName() {
	case codexUserPromptEvent:
		key, prompt := getOneLogAttr(record, codexPromptKey)
		if key == "" || prompt == "" || prompt == codexRedactedUserPrompt {
			return "", nil, nil
		}

		return key, genaiconv.InputMessages{{
			Role: genaiconv.RoleUser,
			Parts: []genaiconv.Part{&genaiconv.TextPart{
				Type:    genaiconv.PartTypeText,
				Content: prompt,
			}},
			Name: nil,
		}}, nil
	default:
		return "", nil, nil
	}
}

func (CodexLog) OutputContent(*otelv1.InboundLogRecord) (string, genaiconv.OutputMessages, error) {
	return "", nil, nil
}

func (CodexLog) SessionID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, "conversation.id")
	return key, value, nil
}

func (CodexLog) ExternalUserEmail(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, userEmailKey)
	return key, value, nil
}

func (CodexLog) ExternalUserID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, vendorUserAccountIDKey)
	return key, value, nil
}

func (CodexLog) ResponseID(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

const (
	codexProvider       = "openai"
	codexSurface        = "codex"
	codexSSEEvent       = "codex.sse_event"
	codexToolDecision   = "codex.tool_decision"
	codexToolCall       = "codex.tool.call"
	codexToolResult     = "codex.tool_result"
	codexEventKindKey   = "event.kind"
	codexKindCompleted  = "response.completed"
	codexKindFailed     = "response.failed"
	codexKindIncomplete = "response.incomplete"
	codexKindError      = "error"
)

func (CodexLog) Provider(*otelv1.InboundLogRecord) (string, string, error) {
	return scopeNameKey, codexProvider, nil
}

func (CodexLog) Surface(*otelv1.InboundLogRecord) (string, string, error) {
	return scopeNameKey, codexSurface, nil
}

func (CodexLog) EventName(record *otelv1.InboundLogRecord) (string, string, error) {
	key, name := logRawEventName(record)
	return key, name, nil
}

// codexEventType maps Codex's event names onto the agent vocabulary. Codex
// reports usage on the SSE event that closes a turn, so only that kind of
// SSE event is a request; the others stay unclassified but named.
func codexEventType(record *otelv1.InboundLogRecord) (string, string) {
	key, name := logRawEventName(record)
	switch name {
	case codexUserPromptEvent:
		return key, EventTypePrompt
	case codexSSEEvent:
		kindKey, kind := getOneLogAttr(record, codexEventKindKey)
		switch kind {
		case codexKindCompleted:
			return kindKey, EventTypeAPIRequest
		case codexKindFailed, codexKindIncomplete, codexKindError:
			return kindKey, EventTypeAPIError
		}
	case codexToolDecision:
		return key, EventTypeToolDecision
	case codexToolCall, codexToolResult:
		return key, EventTypeToolCallResult
	}
	return "", EventTypeUnclassified
}

func (CodexLog) EventType(record *otelv1.InboundLogRecord) (string, string, error) {
	key, kind := codexEventType(record)
	return key, kind, nil
}

func (CodexLog) SubjectID(record *otelv1.InboundLogRecord) (string, string, error) {
	switch _, kind := codexEventType(record); kind {
	case EventTypeAPIRequest, EventTypeAPIError:
		key, value := getOneLogAttr(record, "response.id")
		return key, value, nil
	case EventTypeToolCallResult, EventTypeToolDecision:
		key, value := getOneLogAttr(record, "call_id")
		return key, value, nil
	}
	return "", "", nil
}

// TurnID: Codex states no turn id at all.
func (CodexLog) TurnID(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

func (CodexLog) Model(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, "model")
	return key, value, nil
}

func (CodexLog) ToolName(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, "tool_name")
	return key, value, nil
}

func (CodexLog) Outcome(record *otelv1.InboundLogRecord) (string, string, error) {
	key, kind := codexEventType(record)
	switch kind {
	case EventTypeAPIRequest:
		return key, OutcomeOK, nil
	case EventTypeAPIError:
		return key, OutcomeError, nil
	case EventTypeToolCallResult:
		successKey, success, present := getOneLogBool(record, "success")
		if !present {
			return "", "", nil
		}
		if success {
			return successKey, OutcomeOK, nil
		}
		return successKey, OutcomeError, nil
	case EventTypeToolDecision:
		decisionKey, decision := getOneLogAttr(record, "decision")
		if decision == "reject" {
			return decisionKey, OutcomeRejected, nil
		}
	}
	return "", "", nil
}

func (CodexLog) OutcomeMessage(record *otelv1.InboundLogRecord) (string, string, error) {
	switch _, kind := codexEventType(record); kind {
	case EventTypeAPIError:
		key, value := getOneLogAttrAny(record, "error", "error.message")
		return key, value, nil
	case EventTypeToolCallResult:
		key, value := getOneLogAttr(record, "error")
		return key, value, nil
	}
	return "", "", nil
}

func (CodexLog) Text(record *otelv1.InboundLogRecord) (string, string, error) {
	switch _, kind := codexEventType(record); kind {
	case EventTypePrompt:
		key, value := getOneLogAttr(record, codexPromptKey)
		if value == codexRedactedUserPrompt {
			return "", "", nil
		}
		return key, value, nil
	case EventTypeAPIError:
		key, value := getOneLogAttrAny(record, "error", "error.message")
		return key, value, nil
	case EventTypeToolDecision:
		key, value := getOneLogAttr(record, "decision")
		return key, value, nil
	}
	return "", "", nil
}

func (CodexLog) DurationNano(record *otelv1.InboundLogRecord) (string, int64, error) {
	key, ms := getOneLogFloat64(record, "duration_ms")
	if key == "" {
		return "", 0, nil
	}
	return key, int64(ms * 1e6), nil
}

// InputTokens: Codex reports input_token_count inclusive of cache reads;
// the canonical shape is disjoint.
func (CodexLog) InputTokens(record *otelv1.InboundLogRecord) (string, int64, error) {
	key, inclusive := getOneLogInt64(record, "input_token_count")
	if key == "" {
		return "", 0, nil
	}
	_, cached := getOneLogInt64(record, "cached_token_count")
	input, _ := tokensDisjoint(inclusive, cached)
	return key, input, nil
}

func (CodexLog) OutputTokens(record *otelv1.InboundLogRecord) (string, int64, error) {
	key, value := getOneLogInt64(record, "output_token_count")
	return key, max(value, 0), nil
}

func (CodexLog) CacheReadTokens(record *otelv1.InboundLogRecord) (string, int64, error) {
	key, cached := getOneLogInt64(record, "cached_token_count")
	if key == "" {
		return "", 0, nil
	}
	_, inclusive := getOneLogInt64(record, "input_token_count")
	_, cacheRead := tokensDisjoint(inclusive, cached)
	return key, cacheRead, nil
}

// Codex reports no cache writes and no cost.
func (CodexLog) CacheWriteTokens(*otelv1.InboundLogRecord) (string, int64, error) {
	return "", 0, nil
}

func (CodexLog) CostUSD(*otelv1.InboundLogRecord) (string, float64, error) {
	return "", 0, nil
}

func (CodexLog) QuerySource(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

func (CodexLog) SkillName(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

func (CodexLog) AgentName(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

func (CodexLog) MCPServerName(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

func (CodexLog) MCPToolName(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

func (CodexLog) ExternalOrgID(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}
