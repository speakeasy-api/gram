---
"server": patch
---

The MCP gateway no longer sends a `mcp_server_tools_list` analytics event for every `tools/list` request. No reports used it, and it was the largest share of product analytics volume. Hosted MCP servers still send `mcp_server_count`.
