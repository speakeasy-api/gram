package enrich

import (
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
)

// columnOutcome fills outcome: how it went, in agent vocabulary. Three
// types imply it: an api_response is ok, an api_error is an error and an
// api_refusal is refused, so they are constants here rather than getters.
// Two types state it and are Required: a tool_call_result and a compaction
// carry a success flag. A tool_decision is Recommended: it is rejected when
// the person or a policy said no and carries nothing when it was accepted,
// since the result row carries how the accepted call went, so an accepted
// decision is not counted.
//
// An api_request is absent on purpose: a request records that a call was
// made, not how it went, and must never be given an outcome. A tool_call is
// absent for the same reason, a payload capture belongs to the request it
// captures, and a prompt has no outcome of its own.
func columnOutcome() columnDefinition {
	outcome := getter[string]{log: dialect.LogDialect.Outcome, span: dialect.SpanDialect.Outcome}
	return column[string]{
		key: OutcomeColumnKey,
		byType: perEventType[string]{
			dialect.EventTypeAPIResponse:    constant(dialect.OutcomeOK),
			dialect.EventTypeAPIError:       constant(dialect.OutcomeError),
			dialect.EventTypeAPIRefusal:     constant(dialect.OutcomeRefused),
			dialect.EventTypeToolCallResult: outcome,
			dialect.EventTypeToolDecision:   recommended(outcome),
			dialect.EventTypeCompaction:     outcome,
		},
	}
}

// outcomeIsError is the condition behind the columns that describe what
// went wrong: they are required only of a record whose outcome is an error,
// since a result that succeeded has no error message and that is not a gap.
var outcomeIsError = condition{
	log: func(d dialect.LogDialect, r *otelv1.InboundLogRecord) bool {
		return stated(d.Outcome(r)) == dialect.OutcomeError
	},
	span: func(d dialect.SpanDialect, s *otelv1.InboundSpan) bool {
		return stated(d.Outcome(s)) == dialect.OutcomeError
	},
}
