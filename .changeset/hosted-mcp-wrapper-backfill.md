---
"server": patch
---

Add the `hosted-mcp-wrappers` migration command that backfills `mcp_servers` wrappers and `mcp_endpoints` for hosted (toolset-backed) MCP servers, copies toolset-keyed grants onto the wrapper, and can later move toolset-keyed dependents and retire the toolset-keyed grants.
