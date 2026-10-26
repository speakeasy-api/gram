---
"server": patch
---

A tool call held for approval on a proxied MCP server now comes back as a tool result marked `isError`, with the request token and the steps to request approval, the same shape hosted servers return. It used to come back as a JSON-RPC error, which agents treat as a failure rather than instructions to follow.
