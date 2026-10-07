package enrich

import "github.com/speakeasy-api/gram/server/internal/otel/dialect"

// columnCostUSD fills cost_usd: what the request cost, in dollars, as the
// producer states it. Only an api_request carries usage. Claude Code states
// it in dollars or in micros, which the dialect already reconciles; Codex
// reports no cost at all, so for Codex every request counts as missing
// here, which is the honest reading of the gap.
func columnCostUSD(in *Instruments) LogEnricher {
	return &logColumnEnricher[float64]{
		column: CostUSDColumnKey,
		byType: perEventType[float64]{
			dialect.EventTypeAPIRequest: {log: dialect.LogDialect.CostUSD, span: dialect.SpanDialect.CostUSD},
		},
		instruments: in,
	}
}
