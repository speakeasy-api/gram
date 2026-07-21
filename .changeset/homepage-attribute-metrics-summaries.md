---
"dashboard": patch
---

Serve the project homepage's hook/agent-view metrics (Total Spend, Sessions, Top Users, Most Agent Sessions by User, Most Used Agents) from the pre-aggregated `attribute_metrics_summaries` table via `telemetry.query` instead of paginating every user through `telemetry.searchUsers` (which scanned raw `telemetry_logs`). This is the same source the Costs page uses, so the homepage and Costs figures now agree. The MCP-hosting fallback view is unchanged.
