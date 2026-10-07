package enrich

import "github.com/speakeasy-api/gram/server/internal/otel/dialect"

// columnCacheReadTokens fills cache_read_tokens: the input tokens a request
// read from the prompt cache rather than sending anew. Only an api_request
// carries usage. A stated zero is written, since a request that read
// nothing from the cache says so.
func columnCacheReadTokens() columnDefinition {
	return column[int64]{
		key: CacheReadTokensColumnKey,
		byType: perEventType[int64]{
			dialect.EventTypeAPIRequest: {log: dialect.LogDialect.CacheReadTokens, span: dialect.SpanDialect.CacheReadTokens},
		},
	}
}
