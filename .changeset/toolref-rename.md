---
"server": patch
---

Rename the internal `mcpname` package to `toolref` and route the Codex hook's
MCP tool-name attribution through `toolref.AttributeTool` instead of a
hand-rolled `mcp__<server>__<tool>` split. No behavior change.
