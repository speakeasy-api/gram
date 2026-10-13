---
"server": minor
"dashboard": patch
---

Organization admins can select the EMA provider for a vendor with `organizationRemoteSessionIssuers.setEMAProvider` (behind the `ema-providers` rollout); omitting the provider restores the endpoint's configured external issuer, which stays possible with the rollout off. Speakeasy is used only for vendors it is selected for, and only while the organization's sign-in enforcement is on; it vouches for an eligible sign-in under the organization's allowed sign-in methods. A selected provider that cannot serve never falls back to another one. Issuer and identity-chaining reads report the selected provider and, when it cannot serve, why.
