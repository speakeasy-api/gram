package dialect

import (
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/genaiconv"
)

type NilLog struct{}

func (NilLog) AppliesTo(*otelv1.InboundLogRecord) bool { return false }
func (NilLog) InputContent(*otelv1.InboundLogRecord) (string, genaiconv.InputMessages, error) {
	return "", nil, nil
}
func (NilLog) OutputContent(*otelv1.InboundLogRecord) (string, genaiconv.OutputMessages, error) {
	return "", nil, nil
}
func (NilLog) SessionID(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}
func (NilLog) ExternalUserID(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}
func (NilLog) ExternalUserEmail(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}
func (NilLog) ResponseID(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}

func (NilLog) Provider(*otelv1.InboundLogRecord) (string, string, error)        { return "", "", nil }
func (NilLog) Surface(*otelv1.InboundLogRecord) (string, string, error)         { return "", "", nil }
func (NilLog) EventName(*otelv1.InboundLogRecord) (string, string, error)       { return "", "", nil }
func (NilLog) EventType(*otelv1.InboundLogRecord) (string, string, error)       { return "", "", nil }
func (NilLog) SubjectID(*otelv1.InboundLogRecord) (string, string, error)       { return "", "", nil }
func (NilLog) TurnID(*otelv1.InboundLogRecord) (string, string, error)          { return "", "", nil }
func (NilLog) Model(*otelv1.InboundLogRecord) (string, string, error)           { return "", "", nil }
func (NilLog) ToolName(*otelv1.InboundLogRecord) (string, string, error)        { return "", "", nil }
func (NilLog) Outcome(*otelv1.InboundLogRecord) (string, string, error)         { return "", "", nil }
func (NilLog) OutcomeMessage(*otelv1.InboundLogRecord) (string, string, error)  { return "", "", nil }
func (NilLog) Text(*otelv1.InboundLogRecord) (string, string, error)            { return "", "", nil }
func (NilLog) QuerySource(*otelv1.InboundLogRecord) (string, string, error)     { return "", "", nil }
func (NilLog) SkillName(*otelv1.InboundLogRecord) (string, string, error)       { return "", "", nil }
func (NilLog) AgentName(*otelv1.InboundLogRecord) (string, string, error)       { return "", "", nil }
func (NilLog) MCPServerName(*otelv1.InboundLogRecord) (string, string, error)   { return "", "", nil }
func (NilLog) MCPToolName(*otelv1.InboundLogRecord) (string, string, error)     { return "", "", nil }
func (NilLog) ExternalOrgID(*otelv1.InboundLogRecord) (string, string, error)   { return "", "", nil }
func (NilLog) DurationNano(*otelv1.InboundLogRecord) (string, int64, error)     { return "", 0, nil }
func (NilLog) InputTokens(*otelv1.InboundLogRecord) (string, int64, error)      { return "", 0, nil }
func (NilLog) OutputTokens(*otelv1.InboundLogRecord) (string, int64, error)     { return "", 0, nil }
func (NilLog) CacheReadTokens(*otelv1.InboundLogRecord) (string, int64, error)  { return "", 0, nil }
func (NilLog) CacheWriteTokens(*otelv1.InboundLogRecord) (string, int64, error) { return "", 0, nil }
func (NilLog) CostUSD(*otelv1.InboundLogRecord) (string, float64, error)        { return "", 0, nil }
