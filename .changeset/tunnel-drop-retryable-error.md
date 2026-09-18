---
"server": patch
---

A tunneled MCP server that drops its connection mid-request now returns a JSON-RPC error that says the request may have already run, instead of an HTTP 502.
