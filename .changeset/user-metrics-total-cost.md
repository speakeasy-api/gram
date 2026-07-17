---
"server": patch
---

Fix `TotalCost` (and cache token counts) always returning 0 from `telemetry.getUserMetricsSummary` and `telemetry.getProjectMetricsSummary` — the summary queries never selected the `total_cost`, `cache_read_input_tokens`, and `cache_creation_input_tokens` columns even though cost is attributed on every usage row. Also surface ClickHouse execution errors from the summary queries instead of silently returning an all-zero summary when a query fails mid-stream, which made transient failures look like "no activity" for the requested window.
