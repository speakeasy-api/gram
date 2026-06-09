---
"server": minor
"dashboard": minor
---

Support publishing Remote MCP-backed `mcp_servers` to collections alongside toolset-backed servers. `collections.attachServer` / `collections.detachServer` accept either `toolset_id` or `mcp_server_id` (exactly one), `collections.create` accepts `mcp_server_ids` in addition to `toolset_ids`, `collections.listServers` returns both backends merged by publish time, and `ExternalMCPServer` exposes `mcp_server_id`. In the dashboard, the Publishing section, the create-collection form, and the collection detail edit-servers picker all offer Remote MCP-backed servers, and the Remote MCP server settings page gains a Publishing section.
