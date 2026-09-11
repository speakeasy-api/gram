---
"server": minor
---

Enrich remote sessions from the issuer's userinfo and introspection endpoints: capture identity from userinfo when the exchange returned no ID token, and let Verify mark a grant inactive when the provider's introspection says its token is dead.
