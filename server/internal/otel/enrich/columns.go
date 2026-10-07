package enrich

// columns is every agent_events column a column enricher fills, in column
// order, declared once and served to both signals. One file per column
// holds its table and is the documentation of that column.
func columns() []columnDefinition {
	return []columnDefinition{
		// Who and where.
		columnSessionID(),
		columnTurnID(),
		columnEventID(),
		columnUserEmail(),
		columnExternalUserID(),
		columnExternalOrgID(),

		// What happened.
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
	definitions := columns()
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
	definitions := columns()
	out := make([]SpanEnricher, 0, len(definitions)+1)
	out = append(out, &spanClassification{})
	for _, definition := range definitions {
		out = append(out, definition.span(in))
	}
	return out
}
