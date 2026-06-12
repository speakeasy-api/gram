---
"server": patch
---

Tag assistant memories with provenance. `platform_memory_remember` now records which source surface the memory came from (slack, dashboard, cron, wake), the external user who said it, the channel it was said in, and when it was written. Recall surfaces this as a compact attribution line (e.g. "from slack user U123 in C456, 2026-06-12") so assistants can reason about how much to trust a memory and poisoned writes stay attributable.
