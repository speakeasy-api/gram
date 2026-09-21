---
"server": patch
---

Optionally serve the per-server MCP OAuth authorization server on an alternate authentication host that carries no MCP traffic, configured with `GRAM_MCP_AUTHENTICATION_HOST_URL`. The host answers the authorization server routes for `/mcp` and `/x/mcp` servers (metadata, register, authorize, consent, token and revoke) and returns 404 for everything else. A user session issuer opts in with `use_authentication_host`: its servers then announce the authentication host as their issuer in authorization server metadata, protected resource metadata, authorization responses and minted tokens, while the resource stays on the MCP host. Authorization server metadata is served only on the host its issuer names. The ID-JAG exchange is not accepted on the authentication host.
