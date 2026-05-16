---
"server": minor
"dashboard": patch
---

The playground's Connect button now drives the issuer-gated OAuth flow when a toolset is bound to a user-session issuer, so connecting to MCP servers like `speakeasy-team-github` lands an upstream session that the runtime can resolve. The connection-status badge and the 401 challenge on `/mcp/{slug}` both read from the issuer-gated session store for these toolsets, and the security-check fallback now always emits a non-empty `resource_metadata` URL.
