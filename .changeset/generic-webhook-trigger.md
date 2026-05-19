---
"server": minor
---

Add a generic `webhook` trigger. Vendors describe their signing scheme and field extraction via config (HMAC algorithm + header + encoding + prefix + template + replay window, CEL extractors for event type and correlation id) so triggers for Linear, GitHub, Stripe, and other HMAC-signed webhook sources land without per-vendor Go.
