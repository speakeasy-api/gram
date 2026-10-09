---
"server": patch
---

Linear (`https://linear.app/.well-known/oauth-client-metadata/mcp.json`) is now admitted under presets mode. Linear hosts an MCP client that connects to user MCP servers; on presets-mode issuers, a missing catalog entry is a hard auth failure with no recourse. Verified 2026-10-09: HTTP 200, self-referential client_id, and token_endpoint_auth_method "none".
