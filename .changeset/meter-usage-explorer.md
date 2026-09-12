---
"server": minor
"dashboard": minor
---

Add meter-ledger usage reporting for stored-message tokens, MCP ingress/egress bytes, and risk-scanning volume. The additive `usage.getMeterUsage` API returns exact, dense daily totals and bounded facet breakdowns for windows up to three calendar months. Ordinary usage and signed adjustments remain separate.

The billing explorer now uses these readings while retaining independent contract-position and invoice estimates. API deployments must configure the dedicated `CLICKHOUSE_READ_*` connection with a SELECT-only reader before rollout; meter reporting never falls back to the writer connection.

Daily facet summaries are rebuilt hourly in bounded tenant/time slices with exact deduplication, adaptive subdivision, and atomic whole-generation publication. Failed builds retain the previous complete generation; later passes repair late-arriving readings. Deploy the updated worker alongside the schema migration and provision its staging-table and atomic-exchange permissions separately from the API reader.
