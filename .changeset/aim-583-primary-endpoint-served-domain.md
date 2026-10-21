---
"server": patch
---

The Playground MCP URL and the `iss` claim of dashboard-minted user-session tokens no longer name a custom domain that is not verified and activated. Those addresses fall back to the server's platform endpoint, or to `/mcp/{slug}` when it has none. A toolset whose MCP server's only endpoints sit on such a domain still mints against that server's issuer; only the `iss` URL changes. The legacy toolset Playground URL applies the same check to the toolset's own custom domain.
