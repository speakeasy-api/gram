---
"server": patch
"dashboard": patch
---

Speed up the Skills list, where activation counts took ~1 minute to load. The
`skillEfficacy.queryInsights` endpoint computed attributed session cost by
scanning raw `telemetry_logs` for the whole project window on every request,
even though the list only shows activations, efficacy, and estimated savings.
A new optional `include_costs` flag (default true, so existing SDK/API
consumers are unchanged) lets callers skip that scan; the skills list now opts
out. Regression-signal and suggestion-trend queries, which only compare
efficacy scores, also skip the cost scan.
