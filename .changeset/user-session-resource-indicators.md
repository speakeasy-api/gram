---
"server": minor
---

The user-session OAuth authorization server now validates the RFC 8707 `resource` parameter on both the authorize and token legs of `/mcp/{slug}` and `/x/mcp/{slug}`. A value that does not name the endpoint the request was addressed to is rejected with `invalid_target` — by redirect on `/authorize`, as RFC 6749 §5.2 JSON on `/token` — instead of being accepted and discarded. Clients that omit the parameter are unaffected, which keeps callers predating MCP `2026-07-28` working.

The comparison target is the identifier the endpoint already publishes as its authorization-server `issuer` and its protected-resource `resource`, built from the address each request arrived on rather than from a stored URL, so a server reachable under both a custom domain and the platform origin validates correctly under either. Comparison is byte equality, matching the discipline applied to the RFC 9207 `iss` parameter on the same surface.

Token minting is unchanged: the `aud` claim and bearer validation continue to use the internal resource URN, so tokens already in flight keep working and `/x/mcp` tokens stay portable between toolset-backed and remote-backed servers under one issuer.
