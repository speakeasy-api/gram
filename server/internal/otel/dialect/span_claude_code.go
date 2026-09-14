package dialect

import (
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/genaiconv"
)

type ClaudeCodeSpan struct{}

func (e ClaudeCodeSpan) AppliesTo(span *otelv1.InboundSpan) bool {
	return span.GetScope().GetName() == "com.anthropic.claude_code.tracing"
}

func (e ClaudeCodeSpan) InputContent(span *otelv1.InboundSpan) (key string, val genaiconv.InputMessages, err error) {
	k, v := getOneAttr(span, claudeCodeUserPromptKey)
	if k == "" || v == "" {
		return "", nil, nil
	}

	return k, genaiconv.InputMessages{
		{
			Role: genaiconv.RoleUser,
			Parts: []genaiconv.Part{
				&genaiconv.TextPart{
					Type:    genaiconv.PartTypeText,
					Content: v,
				},
			},
			Name: nil,
		},
	}, nil
}

func (e ClaudeCodeSpan) OutputContent(span *otelv1.InboundSpan) (key string, val genaiconv.OutputMessages, err error) {
	return
}

func (e ClaudeCodeSpan) SessionID(span *otelv1.InboundSpan) (key string, val string, err error) {
	key, val = getOneAttr(span, "session.id")
	return key, val, nil
}

func (e ClaudeCodeSpan) ExternalUserEmail(span *otelv1.InboundSpan) (key string, val string, err error) {
	key, val = getOneAttr(span, userEmailKey)
	return key, val, nil
}

func (e ClaudeCodeSpan) ExternalUserID(span *otelv1.InboundSpan) (key string, val string, err error) {
	key, val = getOneAttr(span, vendorUserAccountIDKey)
	return key, val, nil
}

func (e ClaudeCodeSpan) ResponseID(span *otelv1.InboundSpan) (key string, val string, err error) {
	key, val = getOneAttr(span, "gen_ai.response.id")
	return key, val, nil
}

// Claude Code spans follow the gen_ai semantic conventions, so every
// record-content answer is the semconv reading; only the producer is
// Claude's own.

func (ClaudeCodeSpan) Provider(*otelv1.InboundSpan) (string, string, error) {
	return scopeNameKey, claudeCodeProvider, nil
}

func (ClaudeCodeSpan) Surface(*otelv1.InboundSpan) (string, string, error) {
	return scopeNameKey, claudeCodeSurface, nil
}

func (ClaudeCodeSpan) EventName(span *otelv1.InboundSpan) (string, string, error) {
	return SemconvSpan{}.EventName(span)
}
func (ClaudeCodeSpan) EventType(span *otelv1.InboundSpan) (string, string, error) {
	return SemconvSpan{}.EventType(span)
}
func (ClaudeCodeSpan) SubjectID(span *otelv1.InboundSpan) (string, string, error) {
	return SemconvSpan{}.SubjectID(span)
}
func (ClaudeCodeSpan) TurnID(span *otelv1.InboundSpan) (string, string, error) {
	return SemconvSpan{}.TurnID(span)
}
func (ClaudeCodeSpan) Model(span *otelv1.InboundSpan) (string, string, error) {
	return SemconvSpan{}.Model(span)
}
func (ClaudeCodeSpan) ToolName(span *otelv1.InboundSpan) (string, string, error) {
	return SemconvSpan{}.ToolName(span)
}
func (ClaudeCodeSpan) Outcome(span *otelv1.InboundSpan) (string, string, error) {
	return SemconvSpan{}.Outcome(span)
}
func (ClaudeCodeSpan) OutcomeMessage(span *otelv1.InboundSpan) (string, string, error) {
	return SemconvSpan{}.OutcomeMessage(span)
}
func (ClaudeCodeSpan) Text(span *otelv1.InboundSpan) (string, string, error) {
	return SemconvSpan{}.Text(span)
}
func (ClaudeCodeSpan) QuerySource(span *otelv1.InboundSpan) (string, string, error) {
	return SemconvSpan{}.QuerySource(span)
}
func (ClaudeCodeSpan) SkillName(span *otelv1.InboundSpan) (string, string, error) {
	return SemconvSpan{}.SkillName(span)
}
func (ClaudeCodeSpan) AgentName(span *otelv1.InboundSpan) (string, string, error) {
	return SemconvSpan{}.AgentName(span)
}
func (ClaudeCodeSpan) MCPServerName(span *otelv1.InboundSpan) (string, string, error) {
	return SemconvSpan{}.MCPServerName(span)
}
func (ClaudeCodeSpan) MCPToolName(span *otelv1.InboundSpan) (string, string, error) {
	return SemconvSpan{}.MCPToolName(span)
}
func (ClaudeCodeSpan) ExternalOrgID(span *otelv1.InboundSpan) (string, string, error) {
	return SemconvSpan{}.ExternalOrgID(span)
}
func (ClaudeCodeSpan) DurationNano(span *otelv1.InboundSpan) (string, int64, error) {
	return SemconvSpan{}.DurationNano(span)
}
func (ClaudeCodeSpan) InputTokens(span *otelv1.InboundSpan) (string, int64, error) {
	return SemconvSpan{}.InputTokens(span)
}
func (ClaudeCodeSpan) OutputTokens(span *otelv1.InboundSpan) (string, int64, error) {
	return SemconvSpan{}.OutputTokens(span)
}
func (ClaudeCodeSpan) CacheReadTokens(span *otelv1.InboundSpan) (string, int64, error) {
	return SemconvSpan{}.CacheReadTokens(span)
}
func (ClaudeCodeSpan) CacheWriteTokens(span *otelv1.InboundSpan) (string, int64, error) {
	return SemconvSpan{}.CacheWriteTokens(span)
}
func (ClaudeCodeSpan) CostUSD(span *otelv1.InboundSpan) (string, float64, error) {
	return SemconvSpan{}.CostUSD(span)
}
