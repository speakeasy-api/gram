---
"server": patch
---

The consent page's connect and refresh actions now derive the RFC 8707 resource per remote session client instead of reusing the single endpoint-level resource. Under multi-binding the endpoint-level value belongs to at most one client, so reusing it sent the wrong resource to the wrong upstream authorization server and recorded wrong `remote_sessions.resource` values, breaking resource-matched token routing. A client with no unambiguous upstream now sends and records no resource rather than a wrong one.

Scope: resource-matched routing covers one credential shared across surfaces that front the same upstream. Where a user session issuer fronts two different upstream servers, nothing in the schema says which upstream a client belongs to, so derivation sees both, returns nothing, and the grant is minted without a resource — routing then fails closed rather than serving the wrong upstream. Routing that layout needs a schema addition.
