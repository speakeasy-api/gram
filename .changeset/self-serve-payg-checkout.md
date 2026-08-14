---
"dashboard": patch
"server": patch
---

Allow eligible organization admins to start a self-serve pay-as-you-go Stripe checkout from the billing page. The server reuses a stable Stripe customer, preserves active trials, and records the checkout request in the audit log.
