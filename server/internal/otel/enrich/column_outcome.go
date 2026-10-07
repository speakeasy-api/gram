package enrich

import (
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
)

// columnOutcome fills outcome: how it went, in agent vocabulary. Three
// types imply it: an api_response is ok, an api_error is an error and an
// api_refusal is refused, so they are constants here rather than questions.
// Two types state it: a tool_call_result and a compaction carry a success
// flag. A tool_decision is rejected when the person or a policy said no and
// carries nothing when it was accepted, since the result row carries how the
// accepted call went, so an accepted decision is not counted as missing.
//
// An api_request is absent on purpose: a request records that a call was
// made, not how it went, and must never be given an outcome. A tool_call is
// absent for the same reason, a payload capture belongs to the request it
// captures, and a prompt has no outcome of its own.
func columnOutcome(in *Instruments) LogEnricher {
	outcome := question[string]{log: dialect.LogDialect.Outcome, span: dialect.SpanDialect.Outcome}
	return &logColumnEnricher[string]{
		column: OutcomeColumnKey,
		byType: columnTable[string]{
			dialect.EventTypeAPIResponse:    constant(dialect.OutcomeOK),
			dialect.EventTypeAPIError:       constant(dialect.OutcomeError),
			dialect.EventTypeAPIRefusal:     constant(dialect.OutcomeRefused),
			dialect.EventTypeToolCallResult: outcome,
			dialect.EventTypeToolDecision:   optional(outcome),
			dialect.EventTypeCompaction:     outcome,
		},
		instruments: in,
	}
}

// whenErrored asks a question only of a record whose outcome is an error,
// for the columns that describe what went wrong: a result that succeeded has
// no error message, and that is not a gap.
func whenErrored(q question[string]) question[string] {
	return question[string]{
		log: func(d dialect.LogDialect, r *otelv1.InboundLogRecord) (string, string, error) {
			if stated(d.Outcome(r)) != dialect.OutcomeError {
				return "", "", errNotApplicable
			}
			return q.log(d, r)
		},
		span: func(d dialect.SpanDialect, s *otelv1.InboundSpan) (string, string, error) {
			if stated(d.Outcome(s)) != dialect.OutcomeError {
				return "", "", errNotApplicable
			}
			return q.span(d, s)
		},
	}
}
