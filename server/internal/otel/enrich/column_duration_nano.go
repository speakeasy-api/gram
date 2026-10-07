package enrich

import "github.com/speakeasy-api/gram/server/internal/otel/dialect"

// columnDurationNano fills duration_nano: how long the thing took, when the
// producer states it. An api_request states how long the model call took,
// and a tool_call_result how long the tool ran. A compaction also reports a
// duration, but it is housekeeping rather than a request or a tool, so it
// stays in the payload with its token counts rather than here.
func columnDurationNano(in *Instruments) LogEnricher {
	duration := question[int64]{log: dialect.LogDialect.DurationNano, span: dialect.SpanDialect.DurationNano}
	return &logColumnEnricher[int64]{
		column: DurationNanoColumnKey,
		byType: columnTable[int64]{
			dialect.EventTypeAPIRequest:     duration,
			dialect.EventTypeToolCallResult: duration,
		},
		instruments: in,
	}
}
