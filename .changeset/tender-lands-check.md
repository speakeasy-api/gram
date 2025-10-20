---
"@gram/server": patch
---

Implements listing resources into our actual MCP Server layer. Also implements the gateway proxy for resources currently only being served from functions. Billing/Metrics wise we still treat fetching a resources as a tool call, but there are resource attributes added onto this that would allow us to separate in the future.
