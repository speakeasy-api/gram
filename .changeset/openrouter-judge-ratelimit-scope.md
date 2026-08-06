---
"server": patch
---

Scope the shared LLM judge rate limiter to the OpenRouter key a call spends: platform-key calls share one bucket per model (matching OpenRouter's account-wide shared-capacity limits), while BYOK calls bucket per customer key. This stops chat analysis and risk judges from exhausting OpenRouter's per-model capacity and failing with 429s.
