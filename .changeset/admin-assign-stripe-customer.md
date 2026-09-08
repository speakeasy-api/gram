---
"admin": patch
"server": minor
---

Allow admin operators to assign an initial Stripe customer ID only when both billing identifiers are unset. Fetch live Stripe customer details for an explicit confirmation, revalidate the customer before saving, and atomically audit assignments without overwriting existing billing data.
