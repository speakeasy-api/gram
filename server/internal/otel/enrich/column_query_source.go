package enrich

import "github.com/speakeasy-api/gram/server/internal/otel/dialect"

// columnQuerySource fills query_source: where inside the agent a request
// originated, such as a user prompt or a tool result feeding back into the
// model. It describes the request, so only an api_request carries it. Only
// Claude Code states it today; for other producers every request counts as
// missing here, which is the honest reading of the gap.
func columnQuerySource() columnDefinition {
	return column[string]{
		key: QuerySourceColumnKey,
		byType: columnTable[string]{
			dialect.EventTypeAPIRequest: {log: dialect.LogDialect.QuerySource, span: dialect.SpanDialect.QuerySource},
		},
	}
}
