---
"server": minor
"dashboard": patch
---

Flag inactive MCP servers on the Distribute MCP listing. A new `telemetry.getMcpServerActivity` endpoint reports per-server tool-call activity, and each card/row now shows a subtle indicator when a server has never received a tool call and a warning when it has had no tool calls in the last two weeks.
