---
"server": patch
---

Record the return URL on every Stripe Checkout intent, including intents created on the site host, so a retried checkout replays the same success and cancel URLs after the site URL changes.
