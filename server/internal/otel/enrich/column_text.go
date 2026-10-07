package enrich

import (
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
)

// maxTextBytes bounds the canonical copy of a record's words. The copy sits
// beside the producer's own attribute, so an unbounded one would double a
// large prompt and could push a record that fit when it arrived past the
// relay export limit. 64 KiB holds any prompt a person types and most that
// an agent assembles, while staying well inside the 256 KiB the relay
// export reserves for everything the transform adds.
const maxTextBytes = 64 * constants.KiB

// columnText fills text: the record in words. A prompt's words are the
// prompt, an api_response's the response, an api_error's the error and a
// tool_decision's the decision. An api_request and a tool call put
// everything in attributes, a payload capture keeps its body whole in the
// attributes, and a compaction has nothing to say, so they are absent. The
// writer still promotes a string body for a record no dialect classified,
// since that is the closest thing the producer offered.
//
// Producers log these words only when the person running the agent agreed
// to it, so text is Opt-In on all four types: an absent text is a choice
// rather than a gap and is not counted.
func columnText() columnDefinition {
	text := optIn(getter[string]{log: dialect.LogDialect.Text, span: dialect.SpanDialect.Text})
	return cappedColumn{
		column: column[string]{
			key: TextColumnKey,
			byType: perEventType[string]{
				dialect.EventTypePrompt:       text,
				dialect.EventTypeAPIResponse:  text,
				dialect.EventTypeAPIError:     text,
				dialect.EventTypeToolDecision: text,
			},
		},
		capBytes: maxTextBytes,
	}
}
