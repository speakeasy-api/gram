---
"server": patch
---

When an identity provider rate-limits a company sign-in refresh, Speakeasy now waits as long as the provider's Retry-After asks (at least one minute, at most five) before trying again.
