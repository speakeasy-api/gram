---
"server": patch
---

Re-registering a remote session client that stores no scope now registers the scope its logins request, so providers that only grant registered scopes keep accepting those logins.
