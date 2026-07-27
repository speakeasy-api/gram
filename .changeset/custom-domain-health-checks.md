---
"server": minor
"dashboard": patch
---

Add daily custom-domain routing and TLS certificate health checks in an observation-only first release: checks log their findings, including the admin notifications a future release will send, without persisting health state or emailing anyone yet. The dashboard groundwork for health warnings and a manual recheck ships alongside but stays dormant until observation ends.
