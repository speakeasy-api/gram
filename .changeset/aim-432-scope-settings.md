---
"server": minor
"dashboard": minor
"admin": patch
---

Organization admins can set a remote identity provider to send no scope instead of its whole supported list when a sign-in has no other scope source. The provider's Overview, Settings and the admin issuer and server health views show the setting, and the client-side warning that a client's scopes have no effect under an issuer scope override is gone, since a client's own scopes now take precedence.
