---
"server": patch
---

Relayed MCP requests still waiting on a remote MCP server 45 seconds into a shutdown are now answered with a JSON-RPC error (`-32603`, `data.reason` `server_shutdown`) instead of being cut off, and the remote MCP server is sent a best-effort `notifications/cancelled` on protocol revisions before 2026-07-28. During shutdown a relayed response stream also ends as soon as its response has been relayed. Response streams that go idle or fail before their response is relayed now answer the client with an SSE JSON-RPC error event rather than an error the client could not read, and tool calls interrupted by shutdown are logged with status 503 rather than 502.
