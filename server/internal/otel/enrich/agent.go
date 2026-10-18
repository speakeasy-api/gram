package enrich

// The agent attribute enrichers write the canonical speakeasy.agent.*
// attributes beside a producer's own, for logs and spans alike, so every
// consumer reads one vocabulary. Design doc: "Populating agent_events,
// column by column" in Linear.

const (
	classificationEnricherName = "enrich-classification"
	identityEnricherName       = "enrich-identity"
	operationEnricherName      = "enrich-operation"
	usageEnricherName          = "enrich-usage"
)

// LogAgentAttributes is every enricher that writes an agent attribute for a
// log record, classification first.
func LogAgentAttributes() []LogEnricher {
	return []LogEnricher{
		&logClassification{},
		&logIdentity{},
		&logOperation{},
		&logUsage{},
	}
}

// SpanAgentAttributes is LogAgentAttributes for spans.
func SpanAgentAttributes() []SpanEnricher {
	return []SpanEnricher{
		&spanClassification{},
		&spanIdentity{},
		&spanOperation{},
		&spanUsage{},
	}
}
