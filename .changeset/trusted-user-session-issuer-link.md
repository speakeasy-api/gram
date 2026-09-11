---
"server": minor
---

Allow organization administrators to link a user-session issuer to an organization-level or global trusted remote-session issuer. Remote-issuer lifecycle preflights and mutations now protect active trust links, and metadata refreshes persist and revalidate the trusted issuer's public JWK Set atomically.
