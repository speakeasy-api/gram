---
"dashboard": patch
"server": patch
---

Remove the dormant telemetry.queryRiskTokens endpoint (no consumers; it computed the pre-DNO-491 billed population and no longer matched any billing surface)
