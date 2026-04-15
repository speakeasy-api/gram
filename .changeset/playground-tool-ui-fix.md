---
"@gram-ai/elements": minor
"dashboard": patch
---

Restore the rich tool-call rendering in the playground. The MCP Apps integration had replaced Elements' default tool UI for every tool call; now the playground delegates to the default `ToolFallback` and only appends the MCP App iframe when the tool has a UI resource binding. Elements now exports `ToolFallback` from its public API so consumers can compose around it.
