---
"server": patch
---

A database that cannot answer now returns a retryable 503 on MCP, discovery and token endpoints, not a 500.
