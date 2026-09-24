---
"dashboard": patch
---

Review fixes on the Workload Identities page: reset the admit dialog when a successful admission closes it, so the next one does not open prefilled; offer only active agents, since a suspended or revoked one contributes no policy; validate the issuer and JWKS URLs against the server's https and fully-qualified-domain rules before submit rather than surfacing the refusal as a toast; flag a wildcard rule whose stem ends in whitespace, which is stored verbatim and matches nothing; and make the wildcard label toggle its switch.
