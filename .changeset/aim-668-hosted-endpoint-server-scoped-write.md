---
"server": patch
---

Updating a hosted MCP server's address through the endpoint API now accepts an `mcp:write` grant scoped to that server, as the toolset API did, instead of requiring project-wide `mcp:write`.
