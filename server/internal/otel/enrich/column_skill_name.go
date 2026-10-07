package enrich

import "github.com/speakeasy-api/gram/server/internal/otel/dialect"

// columnSkillName fills skill_name: the skill a request or a tool event was
// made on behalf of, when the producer states one. On an api_request it
// describes the request; on a tool_call, its result and the decision about
// it, Claude Code reports a skill invocation itself as a tool event whose
// parameters name the skill, and that is what the catalog's skill dimension
// counts. The column is deprecated in favour of name, but it stays filled
// here until a skill event type exists for name to carry the skill on; when
// one does, this table empties and name takes over. Most requests and most
// tool events involve no skill, so an absent skill is not counted as
// missing.
func columnSkillName(in *Instruments) LogEnricher {
	skill := optional(question[string]{log: dialect.LogDialect.SkillName, span: dialect.SpanDialect.SkillName})
	return &logColumnEnricher[string]{
		column: SkillNameColumnKey,
		byType: columnTable[string]{
			dialect.EventTypeAPIRequest:     skill,
			dialect.EventTypeToolCall:       skill,
			dialect.EventTypeToolCallResult: skill,
			dialect.EventTypeToolDecision:   skill,
		},
		instruments: in,
	}
}
