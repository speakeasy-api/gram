---
"server": minor
"dashboard": minor
---

Add incremental meter usage reporting for stored-message tokens, MCP ingress/egress bytes, and risk-scanning volume. The additive `usage.getMeterUsage` API returns exact integer daily totals and bounded facet breakdowns for UTC-midnight windows up to three calendar months. The API and dashboard report ordinary usage only; correction readings are excluded.

The billing explorer now uses these readings while retaining independent contract-position and invoice estimates. Totals, daily averages, chart axes, and breakdown rows use compact KTok/MTok/BTok or binary KiB/MiB/GiB units, with full values available on hover. Project breakdowns show available project slugs consistently in charts and tables, falling back to IDs when unavailable. Switching breakdowns keeps the chart and legend synchronized, including transitions between total-only and stacked views. Reset sits beside the period controls and matches the Refresh button's sizing. API deployments must configure the dedicated `CLICKHOUSE_READ_*` connection with a SELECT-only reader before rollout; meter reporting never falls back to the writer connection.

Insert-triggered materialized views maintain daily SummingMergeTree series without historical rebuilds. Queries sum unmerged increments and rank series in storage order with bounded top-six state. Duplicate deliveries count unless prevented by the producer or corrected out of band; raw-table replacement merges do not retract summary contributions. The single schema migration does not backfill existing readings, and no summary worker or publication privileges are required.
