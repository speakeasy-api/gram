---
"server": patch
"dashboard": patch
---

Persist the replayed flag on captured chat messages and surface it on risk results: messages redelivered from a device's offline spool after control-plane downtime (X-Gram-Replayed) now carry chat_messages.replayed, and findings produced by scanning them return replayed on the RiskResult type so retroactive findings are distinguishable from live ones.
