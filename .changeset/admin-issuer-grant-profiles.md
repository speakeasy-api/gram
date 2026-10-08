---
"admin": patch
---

Global identity providers created or rediscovered in the admin app keep the authorization grant profiles their discovery document advertises (including the ID-JAG profile), so identity chaining no longer reports them as unsupported. Providers entered by hand omit the field, so the server does not treat them as discovered. Saving a global provider with a changed issuer URL and no rediscovery sends an empty grant profile list.
