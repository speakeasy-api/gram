---
"server": patch
"dashboard": patch
---

MCP install page metadata for a hosted server is found whether its record is keyed by the toolset or by the toolset's canonical MCP server, so the install page, the MCP runtime, and plugin generation keep working once records move onto the MCP server. Saving metadata updates the existing record under its current key and never creates a second one. `setMcpMetadata` accepts `mcp_server_id` alongside `toolset_slug` when it is that toolset's canonical server, and `exportMcpMetadata` accepts `mcp_server_id` (its `mcp_slug` is now optional).
