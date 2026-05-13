---
"server": patch
---

`/x/mcp` tools/call traffic now writes a structured row to ClickHouse `telemetry_logs` per invocation, mirroring the existing `/mcp` emit. The row carries `gram.remote_mcp_server.id` and `gram.tool.name` attributes so the Source Activity panel for a Remote MCP source can filter telemetry by the originating remote server. Emission is fire-and-forget so ClickHouse latency does not appear in tool-call tail latency.
