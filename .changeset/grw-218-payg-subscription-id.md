---
"server": patch
"admin": patch
---

Staff can record a Stripe subscription ID for a PAYG organization that does not have one yet. The admin API checks that the subscription belongs to the organization's Stripe customer, and stores the billing-cycle anchor Stripe returns, before saving it.
