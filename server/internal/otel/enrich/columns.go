package enrich

// registry is every agent_events column a column enricher fills, in column
// order: the attribute registry, in OpenTelemetry's terms, where each
// column[V] is one attribute definition. A definition is declared once and
// referenced by both signals, the way a semantic-convention attribute group
// is declared once and used by spans and logs alike
// (https://opentelemetry.io/docs/specs/semconv/general/semantic-convention-groups/),
// and it sets a requirement level per event type. One file per column holds
// its definition and is the documentation of that column.
func registry() []columnDefinition {
	return []columnDefinition{
		// Identity: who the event belongs to.
		columnSessionID(),
		columnTurnID(),
		columnEventID(),
		columnUserEmail(),
		columnExternalUserID(),
		columnExternalOrgID(),

		// Operation: what the event was about and how it went.
		columnModel(),
		columnQuerySource(),
		columnSkillName(),
		columnAgentName(),
		columnMCPServerName(),
		columnMCPToolName(),
		columnName(),
		columnToolName(),
		columnText(),
		columnOutcome(),
		columnOutcomeMessage(),
		columnDurationNano(),

		// Usage, which only a request carries.
		columnInputTokens(),
		columnOutputTokens(),
		columnCacheReadTokens(),
		columnCacheWriteTokens(),
		columnCostUSD(),
	}
}

// LogColumns is every enricher that fills an agent_events column from a
// log record: the classification enricher first, since its event type is
// what every table keys on, then one enricher per column. The log transform
// appends them after the tenancy, token and directory enrichers, and the
// agent_events writer reads what they wrote.
//
// The instruments carry the missing-value counter the per-column enrichers
// record into; the classification enricher counts nothing.
func LogColumns(in *Instruments) []LogEnricher {
	definitions := registry()
	out := make([]LogEnricher, 0, len(definitions)+1)
	out = append(out, &logClassification{})
	for _, definition := range definitions {
		out = append(out, definition.log(in))
	}
	return out
}

// SpanColumns is LogColumns for spans: the same columns, from the same
// tables, in the same order, for the span transform.
func SpanColumns(in *Instruments) []SpanEnricher {
	definitions := registry()
	out := make([]SpanEnricher, 0, len(definitions)+1)
	out = append(out, &spanClassification{})
	for _, definition := range definitions {
		out = append(out, definition.span(in))
	}
	return out
}
