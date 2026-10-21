---
"server": patch
---

Drop the toolset-keyed hosted MCP dependents: `plugin_servers.toolset_id`, `mcp_metadata.toolset_id` and the `assistant_toolsets` table. Plugin members must now point at an MCP server or a gateway, and MCP metadata must point at an MCP server.
