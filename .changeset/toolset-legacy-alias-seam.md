---
"server": patch
---

Reads and writes of the legacy toolset `/mcp/<slug>` alias now go through one dedicated package, and generated database code no longer includes the retired toolset MCP hosting columns. Behavior is unchanged.
