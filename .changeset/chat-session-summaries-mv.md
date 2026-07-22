---
"server": patch
---

Add the chat_session_summaries ClickHouse table and materialized view: per-chat hourly session rollups (tokens, cost, message/tool-call counts, status, model, and filter-dimension value sets) so the org-scoped sessions list can read pre-aggregated data instead of scanning raw telemetry_logs.
