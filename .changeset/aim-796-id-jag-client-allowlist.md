---
"server": minor
---

Only Claude Code and Codex, identified by their client ID metadata document URLs, can exchange an ID-JAG for an access token. Other clients get `unauthorized_client` before the assertion is used. `get_inbound_ema_trust` lists the accepted clients and the client ID to configure for each in the identity provider, and warns when an issuer's disabled client ID metadata document admission blocks ID-JAG sign-in. On a shared authorization server, an ID-JAG token request that names no `resource` is routed by the assertion's `resource` claim, so Codex works against the organization gateway.
