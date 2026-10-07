package enrich

import "github.com/speakeasy-api/gram/server/internal/otel/dialect"

// columnOutputTokens fills output_tokens: the tokens the model produced for
// a request. Only an api_request carries usage.
func columnOutputTokens() columnDefinition {
	return column[int64]{
		key: OutputTokensColumnKey,
		byType: columnTable[int64]{
			dialect.EventTypeAPIRequest: {log: dialect.LogDialect.OutputTokens, span: dialect.SpanDialect.OutputTokens},
		},
	}
}
