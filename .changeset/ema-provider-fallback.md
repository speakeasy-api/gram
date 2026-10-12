---
"server": minor
"dashboard": patch
---

Identity chaining now tries each EMA provider allowed for a vendor in order and falls through to the next when one cannot supply a token. Organization admins set the order with `organizationRemoteSessionIssuers.setEMAProviders` (behind the `ema-providers` rollout); the issuer and identity-chaining reads return it, and the XAA readiness tool reports the providers that can serve.
