package enrich

import "github.com/speakeasy-api/gram/server/internal/otel/dialect"

// columnInputTokens fills input_tokens: the input tokens a request used,
// excluding the ones read from the cache, which cache_read_tokens counts.
// Only an api_request carries usage; the dialects already turn Codex's
// cache-inclusive count into this disjoint shape. A compaction is absent on
// purpose: its before and after counts are housekeeping, not usage, and stay
// in the payload.
func columnInputTokens(in *Instruments) LogEnricher {
	return &logColumnEnricher[int64]{
		column: InputTokensColumnKey,
		byType: columnTable[int64]{
			dialect.EventTypeAPIRequest: {log: dialect.LogDialect.InputTokens, span: dialect.SpanDialect.InputTokens},
		},
		instruments: in,
	}
}
