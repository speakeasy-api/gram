---
"server": patch
---

Optionally serve the per-server MCP token endpoint on a dedicated host that carries no MCP traffic, configured with `GRAM_MCP_TOKEN_HOST_URL`. The host answers `POST /mcp/{slug}/token` and returns 404 for everything else. Issuer and resource values stay on the MCP host, a client assertion may name the token URL on the dedicated host, and the ID-JAG exchange is not accepted there.
