package enrich

import "github.com/speakeasy-api/gram/server/internal/otel/dialect"

// columnDurationNano fills duration_nano: how long the thing took, when the
// producer states it. An api_request states how long the model call took,
// and a tool_call_result how long the tool ran. A tool_call carries one at
// the Recommended level: a tool_call that is a span is the whole call, with
// its duration in the span's own timing, while a tool_call that is a log
// record is the moment the call started and has none, which is not a gap.
// A compaction also reports a duration, but it is housekeeping rather than
// a request or a tool, so it stays in the payload with its token counts
// rather than here.
func columnDurationNano() columnDefinition {
	duration := getter[int64]{log: dialect.LogDialect.DurationNano, span: dialect.SpanDialect.DurationNano}
	return column[int64]{
		key: DurationNanoColumnKey,
		byType: columnTable[int64]{
			dialect.EventTypeAPIRequest:     duration,
			dialect.EventTypeToolCall:       recommended(duration),
			dialect.EventTypeToolCallResult: duration,
		},
	}
}
