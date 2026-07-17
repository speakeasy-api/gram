---
"server": minor
---

Warn organization billing contacts before their managed OpenRouter chat credits run out. The periodic credit-usage poll now emails the billing alert contact when usage of the platform-managed chat key crosses 50%, 75%, 90%, and 100% of the monthly cap, with per-threshold monthly dedup so each level alerts once. BYOK organizations and organizations without a billing alert email are skipped.
