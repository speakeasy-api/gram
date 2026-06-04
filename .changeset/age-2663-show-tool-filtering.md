---
"server": minor
"dashboard": minor
---

Add read-only tool filtering visibility on the MCP details page Tools tab. New `mcp:read`-scoped `listToolFilters` methods on the `toolsets` and `mcpServers` services resolve the effective tool variations group (`mcp_servers` then `toolsets`) and return the available filter scopes (tags) with their member tools plus the tools excluded from all filters, mirroring the runtime `?tags=` behavior. The dashboard Tools tab renders a scopes panel above the tool list when filtering is enabled, with per-tag tool membership and a tag chip that filters the list below.
