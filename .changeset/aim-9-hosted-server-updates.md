---
"server": patch
"dashboard": patch
---

`mcpServers.update` accepts hosted MCP servers: name, visibility, user session issuer, tool filtering group and network access mode are written through the backing toolset, so the toolset and the server stay in step. Toolset updates no longer touch a deleted toolset that shares the slug. The server page keeps a hosted server's visibility controls read-only; visibility stays on the toolset page.
