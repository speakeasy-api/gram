---
"server": patch
---

OpenClaw MCP tool calls now show as Shadow MCP calls in Tool Logs, named after the server they reached, instead of as local tools. OpenClaw names MCP tools `<server>__<tool>` and stamps every MCP result with `details.mcpServer`, so ingest reads the server from that marker. This needs no plugin or `speakeasy-hooks` update. A call's pre-call row has no result, so tool usage carries the server across every row of the call and the call stays one row.
