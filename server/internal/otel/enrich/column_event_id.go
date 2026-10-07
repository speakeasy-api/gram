package enrich

import "github.com/speakeasy-api/gram/server/internal/otel/dialect"

// columnEventID fills event_id: the natural identity of the subject the
// event is about, shared across observations of it by design. The table
// asks the one general question, and the dialect answers for the type it
// sees: the transcript message for a prompt and an api_response, the API
// request for an api_request, api_error, api_refusal and the response body
// that answers it, the tool invocation for a tool_call, tool_call_result and
// tool_decision.
//
// An api_request_body and a compaction are absent on purpose. A request body
// is given no id of its own by the producer, and a compaction is not about
// anything but itself, so neither has a subject to name. The writer falls
// back to the record id for them, which is stable across redelivery, so a
// re-observation of the same record never gets a second subject id.
func columnEventID(in *Instruments) LogEnricher {
	subject := question[string]{log: dialect.LogDialect.SubjectID, span: dialect.SpanDialect.SubjectID}
	return &logColumnEnricher[string]{
		column: EventIDColumnKey,
		byType: columnTable[string]{
			dialect.EventTypePrompt:          subject,
			dialect.EventTypeAPIRequest:      subject,
			dialect.EventTypeAPIResponse:     subject,
			dialect.EventTypeAPIError:        subject,
			dialect.EventTypeAPIRefusal:      subject,
			dialect.EventTypeToolCall:        subject,
			dialect.EventTypeToolCallResult:  subject,
			dialect.EventTypeToolDecision:    subject,
			dialect.EventTypeAPIResponseBody: subject,
		},
		instruments: in,
	}
}
