---
"server": patch
---

Serve the org-scoped sessions list from the chat_session_summaries materialized view on windows of 48 hours or more, keeping narrow windows on the raw telemetry_logs scan. Filters, sorting, and cursor pagination translate onto the pre-aggregated table, and a sync test pins the shared session predicates across the Go constants and the MV definition.
