---
"server": minor
"dashboard": patch
---

`telemetry.getObservabilityOverview` takes `tool_calls_only`. When set, chat, token, cost and session duration figures come back as 0, and behind the `mcp-overview-summary-table` flag a server-scoped overview of more than an hour reads pre-summed tool call counts instead of scanning raw logs.
