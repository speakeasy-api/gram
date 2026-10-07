package enrich

import "github.com/speakeasy-api/gram/server/internal/otel/dialect"

// columnName fills name: the name of the subject of the event, the generic
// pair to event_id. Today the subject with a name is the tool, on a
// tool_call, its result and the decision about it, MCP or not, so those
// three types ask the dialect for the tool's name. A prompt and the api_*
// types are about a message or a model call, which have ids but no name,
// so they are absent. When a skill event type and a sub-agent event type
// exist, they join this table with the skill and the agent as the subject,
// and the per-family skill_name and agent_name columns retire.
func columnName() columnDefinition {
	tool := getter[string]{log: dialect.LogDialect.ToolName, span: dialect.SpanDialect.ToolName}
	return column[string]{
		key: NameColumnKey,
		byType: columnTable[string]{
			dialect.EventTypeToolCall:       tool,
			dialect.EventTypeToolCallResult: tool,
			dialect.EventTypeToolDecision:   tool,
		},
	}
}
