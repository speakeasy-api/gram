---
"server": patch
---

Function, HTTP and external MCP tool calls and function resource reads that Speakeasy runs itself are now answered with a JSON-RPC error (`-32603`, `data.reason` `server_shutdown`) when they are still running 45 seconds into a shutdown, instead of being cut off when the shutdown deadline closes their connection. Tool calls are marked `retryable: false` and resource reads `retryable: true`. Interrupted calls keep their telemetry log and tool call record, which show status 503 and the `gram.tool_call.interrupt_reason` attribute, log a warning, and count on the `mcp.tool_call.shutdown_interrupted` metric. Platform tools, which run inside the server, are left to finish.
