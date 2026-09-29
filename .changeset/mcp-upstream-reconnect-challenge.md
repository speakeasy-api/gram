---
"server": patch
---

When the upstream connection behind an issuer-gated MCP server can no longer be refreshed (for example, the provider revoked its refresh token), MCP clients are now directed to reauthorize instead of retrying the rejected request. The 401 explains that the upstream must be reconnected and carries `error="invalid_token"` for refreshable sessions, and the token endpoint declines to refresh those sessions until the upstream is reconnected. Temporary upstream token endpoint failures now return 503 with `Retry-After`, and upstream client configuration errors tell the user to contact the MCP server administrator.
