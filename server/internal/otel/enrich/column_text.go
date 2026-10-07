package enrich

import "github.com/speakeasy-api/gram/server/internal/otel/dialect"

// columnText fills text: the record in words. A prompt's words are the
// prompt, an api_response's the response, an api_error's the error and a
// tool_decision's the decision. An api_request and a tool call put
// everything in attributes, a payload capture keeps its body whole in the
// attributes, and a compaction has nothing to say, so they are absent. The
// writer still promotes a string body for a record no dialect classified,
// since that is the closest thing the producer offered.
//
// Producers log these words only when the person running the agent agreed
// to it, so an absent text is a choice rather than a gap and is not counted.
func columnText(in *Instruments) LogEnricher {
	text := optional(question[string]{log: dialect.LogDialect.Text, span: dialect.SpanDialect.Text})
	return &logColumnEnricher[string]{
		column: TextColumnKey,
		byType: columnTable[string]{
			dialect.EventTypePrompt:       text,
			dialect.EventTypeAPIResponse:  text,
			dialect.EventTypeAPIError:     text,
			dialect.EventTypeToolDecision: text,
		},
		instruments: in,
	}
}
