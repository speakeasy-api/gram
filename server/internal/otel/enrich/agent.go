package enrich

import "github.com/speakeasy-api/gram/server/internal/otel/dialect"

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

// classifiedEventTypes is the agent vocabulary without the unclassified type.
var classifiedEventTypes = []string{
	dialect.EventTypePrompt,
	dialect.EventTypeAPIRequest,
	dialect.EventTypeAPIResponse,
	dialect.EventTypeAPIError,
	dialect.EventTypeAPIRefusal,
	dialect.EventTypeToolCall,
	dialect.EventTypeToolCallResult,
	dialect.EventTypeToolDecision,
	dialect.EventTypeAPIRequestBody,
	dialect.EventTypeAPIResponseBody,
	dialect.EventTypeCompaction,
}

// LogAgentAttributes is every enricher that writes an agent attribute for a
// log record, classification first.
func LogAgentAttributes(in *Instruments) []LogEnricher {
	return []LogEnricher{
		&logClassification{},
		&logIdentity{instruments: in},
		&logOperation{instruments: in},
		&logUsage{instruments: in},
	}
}

// SpanAgentAttributes is LogAgentAttributes for spans.
func SpanAgentAttributes(in *Instruments) []SpanEnricher {
	return []SpanEnricher{
		&spanClassification{},
		&spanIdentity{instruments: in},
		&spanOperation{instruments: in},
		&spanUsage{instruments: in},
	}
}
