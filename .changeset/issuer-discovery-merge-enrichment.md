---
"server": minor
"dashboard": minor
---

Issuer metadata discovery now probes every well-known candidate and merges same-issuer OpenID Connect and RFC 8414 documents, so fields a provider publishes only in one of them (`jwks_uri`, `claims_supported`, ID token signing algorithms) are captured. Discovery and refresh also record the issuer's userinfo and introspection endpoints, back-channel logout and RFC 9207 support, and keep every member of the merged documents.
