---
"server": minor
"dashboard": patch
---

Identity chaining now tries each identity provider allowed for a vendor in order and falls through to the next when one cannot supply a token. Organization admins can read and set the order per vendor with `getVendorIdentityProviders` and `setVendorIdentityProviders`, and the XAA readiness tool reports it.
