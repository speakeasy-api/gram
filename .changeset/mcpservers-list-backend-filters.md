---
"server": minor
---

Add optional `remote_mcp_server_id` and `toolset_id` filter parameters to `mcpServers.list` so callers can scope the result to MCP servers backed by a single remote MCP server or toolset. The two filters are mutually exclusive.
