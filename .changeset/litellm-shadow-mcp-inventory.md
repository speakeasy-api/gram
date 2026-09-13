---
"server": minor
"dashboard": patch
---

Shadow MCP inventory now includes MCP servers observed through a LiteLLM proxy. Servers LiteLLM knows only by tool namespace appear as `tool_namespace` rows (observe-only, identity unresolved); servers reached through the LiteLLM MCP gateway or declared as hosted-MCP tools appear as normal URL rows. Inventory rows report the hook sources that observed them.
