---
"server": patch
---

Identity chaining now requests the same scopes as an interactive sign-in on the server's connection (the client's scope, the resource's challenge or pinned scopes, or what the resource and its authorization server advertise), minus OpenID Connect scopes, instead of a separate list recorded on the binding. Pinned scopes on the server now narrow chained tokens too. prepareEMA still accepts `scopes` for compatibility and records them, but identity chaining no longer uses them; readEMA, prepareEMA, the Okta resource connection view and Platform MCP's get_xaa_readiness report the scopes actually requested. A binding whose client no sign-in on its issuer uses keeps its recorded scopes.
