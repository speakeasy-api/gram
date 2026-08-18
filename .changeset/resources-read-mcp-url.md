---
"server": patch
---

MCP `resources/read` requests now record the actual MCP server URL in billing telemetry and logs instead of the MCP session id, so resource reads are attributable to the server that served them and the MCP URL filter is no longer polluted with random UUIDs.
