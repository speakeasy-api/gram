---
"server": patch
---

The enabled MCP server count behind the billing meter, the usage display and first-server auto-enable reads hosted servers from their `mcp_servers` wrapper, falling back to the toolset flag for a hosted server that has no wrapper yet. Counts are unchanged while a wrapper matches its toolset, which every write keeps true.
