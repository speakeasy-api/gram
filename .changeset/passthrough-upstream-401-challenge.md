---
"server": patch
---

MCP servers using an external OAuth server now answer a `tools/call` with HTTP 401 and `WWW-Authenticate: Bearer resource_metadata="…", error="invalid_token"` when the upstream API rejects the forwarded access token. MCP clients previously received an HTTP 200 `isError` result, so they kept sending the expired token instead of refreshing or reauthorizing.
