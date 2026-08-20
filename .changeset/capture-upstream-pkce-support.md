---
"server": minor
"dashboard": patch
---

Remote session issuers now capture the upstream's advertised PKCE support (RFC 8414 `code_challenge_methods_supported`) through discovery, refresh, and the create/update forms, and report it without refusing anything. The value is exposed on issuer reads and drafts (serialized without `omitempty` so null, meaning never captured, stays distinct from an empty array), rendered read-only on the issuer Overview tab, and instrumented with an unsampled `gram.remote_session.upstream_authorize` counter dimensioned by the issuer's PKCE support state at every authorize-URL build. Discovery also warns operators when an identity provider does not advertise S256, since MCP requires clients to verify PKCE support and a future change may enforce it. A null value ("never captured") stays distinct from an empty array ("the issuer advertises no methods") end to end.
