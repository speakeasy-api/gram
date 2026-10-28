---
"server": minor
---

ID-JAG exchanges now accept only clients that use a client ID metadata document and that the issuer's existing client admission gate allows (open, presets with custom URLs, or disabled). Dynamically registered and other non-document clients get `unauthorized_client` before the assertion is used. On a shared authorization server, an ID-JAG token request that names no `resource` is routed by the assertion's `resource` claim, so Codex works against the organization gateway. `get_inbound_ema_trust` reports each issuer's admission mode, its custom client URLs in presets mode and the preset clients, and warns when disabled admission blocks ID-JAG sign-in.
