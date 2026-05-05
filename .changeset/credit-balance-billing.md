---
"server": minor
"dashboard": patch
---

Add a credit-balance gate on `/chat/completions` for **free-tier** orgs: pre-request check returns HTTP 402 `insufficient_credits` once the cached Polar Chat Credits balance is exhausted. Pro and enterprise stay bounded by the existing OpenRouter monthly key cap; unifying the two limit sources is tracked separately. Speakeasy-internal orgs (`specialLimitOrgs`) bypass; cache misses fail open. Self-serve top-up checkout (`usage.createTopUpCheckout`) opens a one-time Polar product configured via `POLAR_PRODUCT_IDS_TOPUP`.
