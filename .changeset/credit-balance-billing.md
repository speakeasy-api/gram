---
"server": minor
"dashboard": patch
"@gram/client": minor
---

Add token-balance billing on /chat/completions: pre-request gate returns 402 `insufficient_credits` when an org's cached Polar credit balance is exhausted, OpenRouter cost is debited to the credits meter in $0.001 units, and a self-serve top-up checkout (`usage.createTopUpCheckout`) opens a one-time Polar product. Special Speakeasy-internal orgs bypass the gate; cache misses fail open (the OpenRouter monthly key limit remains the hard backstop).
