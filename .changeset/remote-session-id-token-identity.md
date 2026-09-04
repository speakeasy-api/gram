---
"server": patch
---

Capture who an upstream grant belongs to from the OpenID Connect ID token an issuer returns at code exchange or refresh. The token is verified against the issuer's published key set and reduced to its claims; the consent card shows the result as "Signed in as". Non-standard token response members are kept alongside, minus anything credential-shaped.
