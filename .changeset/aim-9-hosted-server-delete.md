---
"server": patch
"dashboard": patch
---

`mcpServers.delete` on a hosted MCP server now disables it through its backing toolset, as turning MCP off on the toolset does, instead of rejecting the call. The server stays listed as disabled and keeps its endpoint, so it can be re-enabled at the same address; a custom domain root is released as on any disable. The server page no longer offers Delete for hosted servers.
