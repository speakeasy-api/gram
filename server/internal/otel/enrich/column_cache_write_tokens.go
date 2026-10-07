package enrich

import "github.com/speakeasy-api/gram/server/internal/otel/dialect"

// columnCacheWriteTokens fills cache_write_tokens: the input tokens a
// request wrote into the prompt cache. Only an api_request carries usage.
// Claude Code states it on every request; Codex reports no cache writes at
// all, so for Codex every request counts as missing here, which is the
// honest reading of the gap.
func columnCacheWriteTokens(in *Instruments) LogEnricher {
	return &logColumnEnricher[int64]{
		column: CacheWriteTokensColumnKey,
		byType: columnTable[int64]{
			dialect.EventTypeAPIRequest: {log: dialect.LogDialect.CacheWriteTokens, span: dialect.SpanDialect.CacheWriteTokens},
		},
		instruments: in,
	}
}
