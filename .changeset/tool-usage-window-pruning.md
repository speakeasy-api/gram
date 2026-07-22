---
"server": patch
---

Speed up the MCP & Tools observability endpoints (INC-417): the tool-usage and tool-logs queries over `trace_summaries` now add a slop-padded `WHERE start_time_unix_nano` pre-filter so the new minmax skip index prunes the scan to roughly the query window instead of the project's full 90-day history (the exact window predicate stays in `HAVING` over the per-trace minimum), and `GetToolUsageSummary` / `GetToolUsageFilterOptions` run their independent aggregate queries concurrently instead of sequentially.
