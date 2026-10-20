---
"server": patch
"dashboard": patch
---

A configured MCP server header can no longer read the caller's `Authorization` request header as its source. That header may carry the caller's Speakeasy credential, and must not be sent to an upstream MCP server under another name. Creating or updating such a header is refused, and the dashboard flags it while editing. At request time, an optional one is not sent and a required one fails the request. To pass a caller-supplied upstream credential through, read it from a separate request header; it can still be sent upstream as `Authorization`.
