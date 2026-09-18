---
"server": patch
---

The MCP OAuth token endpoint now checks `grant_type` before it authenticates the client. A request with an unsupported `grant_type` gets `unsupported_grant_type` even when it has no client credentials or has invalid ones. The `authorization_code` and `refresh_token` grants still require full client authentication. A JWT bearer request that has client credentials is still an authenticated ID-JAG exchange. A JWT bearer request with no client credentials gets `invalid_client`, the same response as before.
