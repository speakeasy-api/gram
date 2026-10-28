---
"server": minor
---

ID-JAG exchanges now accept only clients that use a client ID metadata document and that the issuer's existing client admission allows in open or presets mode; disabled admits none. Dynamically registered and other non-document clients get `unauthorized_client` before the assertion is used, and every client refusal on this grant is logged and counted as an ID-JAG rejection. On a shared authorization server, an ID-JAG token request that names no `resource` is routed by the assertion's `resource` claim, so Codex works against the organization gateway; an unreadable assertion there gets `invalid_grant`. `get_inbound_ema_trust` reports each issuer's admission mode, its custom client URLs and the preset clients, and explains what presets or disabled mode means for ID-JAG sign-in.
