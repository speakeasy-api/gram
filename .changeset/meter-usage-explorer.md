---
"server": minor
"dashboard": minor
---

Add meter-ledger usage reporting for stored-message tokens, MCP ingress/egress bytes, and risk-scanning volume. The additive `usage.getMeterUsage` API returns exact, dense daily totals and bounded facet breakdowns for windows up to three calendar months. Ordinary usage and signed adjustments remain separate.

The billing explorer now uses these readings while retaining independent contract-position and invoice estimates. API deployments must configure the dedicated `CLICKHOUSE_READ_*` connection with a SELECT-only reader before rollout; meter reporting never falls back to the writer connection.
