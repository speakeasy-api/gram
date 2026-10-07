package enrich

import "github.com/speakeasy-api/gram/server/internal/otel/dialect"

// columnToolName fills tool_name the same way name is filled: the tool on a
// tool_call, its result and the decision about it. The column is deprecated
// in favour of name and stays filled, from the same question on the same
// types, until a contract migration drops it, so nothing that reads it
// today goes quiet.
func columnToolName(in *Instruments) LogEnricher {
	tool := question[string]{log: dialect.LogDialect.ToolName, span: dialect.SpanDialect.ToolName}
	return &logColumnEnricher[string]{
		column: ToolNameColumnKey,
		byType: columnTable[string]{
			dialect.EventTypeToolCall:       tool,
			dialect.EventTypeToolCallResult: tool,
			dialect.EventTypeToolDecision:   tool,
		},
		instruments: in,
	}
}
