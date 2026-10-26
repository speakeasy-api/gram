---
"server": minor
"dashboard": minor
---

A gateway's Overview now shows how its traffic divides between discovery and tool calls in every discovery mode. Tiles give each mode's execution share with its change against the previous period, a chart shows how that share grows as sessions progress, and calls over time can be narrowed to one mode. Gateway telemetry now records each connection's discovery mode and MCP session, Code Mode helper calls and code runs, and `getMetaMcpServerUsage` returns the breakdown by mode, by position in the session, and over time.
