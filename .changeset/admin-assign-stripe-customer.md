---
"admin": patch
"server": minor
---

Allow admin operators to assign an initial Stripe customer ID only when both billing identifiers are unset. Fetch customer details from Stripe for an explicit confirmation and revalidate the customer before saving, without overwriting existing billing data.
