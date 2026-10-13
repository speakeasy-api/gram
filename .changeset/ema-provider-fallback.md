---
"server": minor
"dashboard": patch
---

Organization admins can explicitly select one EMA provider for a vendor with `organizationRemoteSessionIssuers.setEMAProviders` (behind the `ema-providers` rollout). Speakeasy issues ID-JAGs from an eligible enterprise SSO grant when selected. An empty selection retains the endpoint's configured external issuer; failures never switch to another provider automatically. Issuer and identity-chaining reads report the selected provider and its current readiness.
