---
"server": minor
"dashboard": patch
---

Add a built-in preset exclusion library that suppresses known false positives (test credit cards, example API keys/tokens, module/content hashes, placeholder emails) across all detection sources. Adds the `risk.listBuiltinPresets` endpoint and a read-only "Built-in library" section on the Exclusions tab that lists the live catalog.
