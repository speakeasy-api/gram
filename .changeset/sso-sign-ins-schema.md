---
"server": patch
---

Add storage for Speakeasy sign-ins and link MCP grants to the sign-in that created them, so revoking a sign-in can revoke its grants.

Also record the evidence that decides which sign-in methods may back a signed assertion (second factor on password sign-ins, the social identity provider's managed domain and tenant), and let each organization limit those methods on its issuer.
