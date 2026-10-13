---
"server": patch
---

`mcpEndpoints.update` can now change the address of a hosted MCP server's endpoint. It applies the same rules as changing the toolset's MCP slug or custom domain and keeps the toolset in sync. Creating a second endpoint for a hosted server, or deleting its endpoint, is still rejected.
