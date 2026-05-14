---
"dashboard": patch
---

The Source Activity panel on the Remote MCP source overview now shows real telemetry for the last 7 days, scoped to that remote server via the new `remote_mcp_server_id` filter. `TelemetrySummaryRow` and `ToolBarList` are extracted into a shared `SourceActivityPanel` component consumed by both the OpenAPI and Remote MCP source overview tabs.
