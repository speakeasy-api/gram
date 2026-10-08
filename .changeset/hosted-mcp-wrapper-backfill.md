---
"server": patch
---

Add the `hosted-mcp-wrappers` migration command, which gives hosted MCP servers created before canonical wrappers existed their MCP server record and endpoint through the same sync every toolset edit runs, auditing the writes as a system actor.
