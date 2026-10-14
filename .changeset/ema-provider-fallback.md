---
"server": minor
"dashboard": patch
---

Identity chaining tries a vendor's EMA providers in order until one succeeds. Providers that cannot serve, or that the endpoint's authorization server does not trust, are skipped, and a transient failure is retried briefly before the next provider, all within the request deadline. Organization admins set the order with `organizationRemoteSessionIssuers.setEMAProviders` (behind the `ema-providers` rollout); an empty order restores the endpoint's configured external issuer and stays possible with the rollout off. Speakeasy serves only vendors whose order names it, and only while the organization's sign-in enforcement is on. Issuer and identity-chaining reads report the order with each provider's availability.
