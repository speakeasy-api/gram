package enrich

import "github.com/speakeasy-api/gram/server/internal/otel/dialect"

// columnAgentName fills agent_name: the sub-agent a request was made by, or
// the sub-agent a tool event started, when the producer states one. Like
// skill_name it describes the request or the tool event rather than the
// subject, and Claude Code reports starting a sub-agent as a tool event
// whose parameters name the agent. The column is deprecated in favour of
// name and stays filled here until a sub-agent event type exists for name
// to carry the agent on. Most requests come from the main thread and most
// tool events start no sub-agent, so an absent agent is not counted as
// missing.
func columnAgentName(in *Instruments) LogEnricher {
	agent := optional(question[string]{log: dialect.LogDialect.AgentName, span: dialect.SpanDialect.AgentName})
	return &logColumnEnricher[string]{
		column: AgentNameColumnKey,
		byType: columnTable[string]{
			dialect.EventTypeAPIRequest:     agent,
			dialect.EventTypeToolCall:       agent,
			dialect.EventTypeToolCallResult: agent,
			dialect.EventTypeToolDecision:   agent,
		},
		instruments: in,
	}
}
