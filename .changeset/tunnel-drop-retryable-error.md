---
"server": patch
---

A tunneled MCP server that drops its connection mid-request now returns a retryable JSON-RPC error instead of an HTTP 502.
