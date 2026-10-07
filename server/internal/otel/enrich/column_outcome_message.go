package enrich

import "github.com/speakeasy-api/gram/server/internal/otel/dialect"

// columnOutcomeMessage fills outcome_message: the producer's message about
// an outcome that went wrong. An api_error carries the error and is
// Required. A tool_call_result carries the tool's error or its category when
// the full error is not logged, and a compaction the reason it failed; on
// those two the message is Conditionally Required on the outcome being an
// error, since a result or a compaction that succeeded has no message to
// carry. The types whose outcome is implied or absent have no message at
// all.
func columnOutcomeMessage() columnDefinition {
	message := getter[string]{log: dialect.LogDialect.OutcomeMessage, span: dialect.SpanDialect.OutcomeMessage}
	return column[string]{
		key: OutcomeMessageColumnKey,
		byType: perEventType[string]{
			dialect.EventTypeAPIError:       message,
			dialect.EventTypeToolCallResult: conditionallyRequired(message, outcomeIsError),
			dialect.EventTypeCompaction:     conditionallyRequired(message, outcomeIsError),
		},
	}
}
