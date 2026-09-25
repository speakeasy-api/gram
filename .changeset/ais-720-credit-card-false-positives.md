---
"server": patch
---

Stops Presidio credit-card detection from flagging published payment-processor test card numbers, placeholder digit patterns, unissued card prefixes, and card-shaped digit runs in payloads that never mention payments. Applies to both the Go scanner and the pystreams realtime path.
