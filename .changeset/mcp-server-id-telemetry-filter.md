---
"server": minor
---

Add `mcp_server_id` as an optional filter on the observability overview query surface (`getObservabilityOverview`), threaded through the ClickHouse telemetry builders, the Goa payload, and the logs platform tool. A single `mcp_server_id` scopes a fronting MCP server's activity across both remote-backed and toolset-backed sources.
