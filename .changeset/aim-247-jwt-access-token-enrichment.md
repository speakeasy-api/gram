---
"server": minor
---

Enrich remote sessions from verified JWT access tokens (RFC 9068): when the exchange or refresh returns no ID token and userinfo names no identity, a JWT access token signed by the issuer's published keys supplies the subject, email, and scopes. JWKS consumers now honour an explicit key_ops, keeping a key only when it names verify.
