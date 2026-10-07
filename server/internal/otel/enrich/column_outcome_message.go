package enrich

import "github.com/speakeasy-api/gram/server/internal/otel/dialect"

// columnOutcomeMessage fills outcome_message: the producer's message about
// an outcome that went wrong. An api_error carries the error, a
// tool_call_result the tool's error or its category when the full error is
// not logged, and a compaction the reason it failed. A result or a
// compaction that succeeded has no message to carry, so only an errored one
// is asked; the types whose outcome is implied or absent have no message at
// all.
func columnOutcomeMessage(in *Instruments) LogEnricher {
	message := question[string]{log: dialect.LogDialect.OutcomeMessage, span: dialect.SpanDialect.OutcomeMessage}
	return &logColumnEnricher[string]{
		column: OutcomeMessageColumnKey,
		byType: columnTable[string]{
			dialect.EventTypeAPIError:       message,
			dialect.EventTypeToolCallResult: whenErrored(message),
			dialect.EventTypeCompaction:     whenErrored(message),
		},
		instruments: in,
	}
}
