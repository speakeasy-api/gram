---
"dashboard": patch
---

Identity providers created or refreshed in the dashboard from discovered metadata keep the authorization grant profiles they advertise (including the ID-JAG profile), so identity chaining no longer reports them as unsupported. A discovered issuer that advertises no profiles sends an empty list, so it is still recorded as discovered; providers entered by hand omit the field. Changing a provider's issuer URL without rediscovering clears its grant profiles. The Settings tab does the same, and the OAuth proxy wizard no longer substitutes its defaults for lists a discovered document omits, while still keeping the scopes the operator entered.
