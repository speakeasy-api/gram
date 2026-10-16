---
"server": patch
---

Retires the experimental `/x/mcp` MCP surface. Every `/x/mcp/*` route, including its OAuth endpoints, `.well-known` metadata and the `/x/mcp/idp_callback` and `/x/mcp/remote_login_callback` callbacks, now returns 404; MCP servers are served at `/mcp/{slug}`. Dashboard-minted user-session tokens for an MCP server without an endpoint now name `/mcp/{slug}` as their issuer. Tokens already minted with an `/x/mcp` issuer are accepted on `/mcp` exactly as before. Login flows still in progress on `/x/mcp` at deploy, and shared-authorization-server sessions bound to an `/x/mcp` resource, must re-authorize on `/mcp`.
