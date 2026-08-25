---
"server": minor
---

Treat a remote-session credential as one shared upstream grant per subject and remote-session client, with the minting user-session issuer recorded as provenance only. Status, consent auto-refresh, explicit refresh, disconnect, user-session revoke cascades, and the scheduled refresh keepalive now operate through the requesting issuer's tenant-scoped client binding, so every bound surface sees and controls the same credential and a disconnect or revoke from any of them destroys it globally (with a best-effort upstream revocation). Upstream token resolution now returns per-issuer entries qualified by the credential's grant-time RFC 8707 resource, and proxied MCP backends route the matching token per upstream instead of failing closed on more than one — ambiguous or unmatchable selections still fail closed.
