---
"server": patch
---

Report PAYG tokens-under-management usage to Stripe from durable hourly snapshots. Signed meter intents retry safely, reconcile ambiguous delivery against Stripe summaries, freeze the billed baseline after 48 hours, and record one carry-forward correction after the 72-hour observation window.
