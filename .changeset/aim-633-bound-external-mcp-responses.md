---
"server": patch
---

Responses from external MCP servers are now size-limited during connection, tool discovery, and tool calls. Each JSON response body, error body, or single server-sent event may be at most 16 MiB; a larger one fails the request with a "response body exceeds the configured read limit" error instead of being buffered in full. Long-lived event streams keep working, since the limit applies to each event rather than to the whole stream.
