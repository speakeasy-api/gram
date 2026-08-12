---
"dashboard": patch
---

Add a PAYG rate adjustment (%) input to the platform-admin TUM contract price estimator on the billing page. A positive percentage uplifts every pay-as-you-go band rate and a negative one discounts them, so an admin can price a negotiated swing off the list rates without retyping the bands. The adjustment is reflected in the PAYG card's tier table, blended rate, and the committed-vs-PAYG comparison, and a discount-driven "PAYG is cheaper" outcome is attributed to the adjustment instead of being flagged as a pricing-model error.
