---
"server": minor
"dashboard": minor
---

Revoke Remote Session credentials upstream via RFC 7009. Remote session issuers gain a `revocation_endpoint`, discovered from the issuer's RFC 8414 metadata document during issuer refresh and editable on the issuer settings form. When a Remote Session is revoked, Gram now posts the stored token to the issuer's revocation endpoint so the upstream authorization server drops it, instead of leaving a live access/refresh pair that keeps working elsewhere until it expires on its own clock.

This covers every path that ends a session: revoking one session, revoking all of a client's sessions, and deleting a client, which cascades a soft-delete to its sessions. Batches run under bounded concurrency and a single budget for the whole batch, since every session on a client shares one upstream host.

The upstream call is best-effort by construction: it runs after the local revoke has committed, is bounded by a short timeout, and routes through the guardian egress policy. Failures are logged and metered, never surfaced to the caller — the local revoke is the security control the caller asked for and it has already succeeded. Issuers that advertise no revocation endpoint are recorded as a distinct `skipped` metric outcome rather than folded into success or failure, since that is the expected case for a large share of upstreams. A batch that exhausts its budget before reaching every session records the remainder as `dropped` rather than passing them off as done.
