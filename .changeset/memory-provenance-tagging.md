---
"server": patch
---

Tag assistant memories with provenance for tracing. `platform_memory_remember` now records which source surface the memory came from (slack, dashboard, cron, wake), the external user who said it, the origin thread's correlation id, and when it was written. Recall surfaces this as a compact attribution line (e.g. "from slack user U123 (slack:T123:C456:789.012), 2026-06-12") so it is always possible to answer "why is the assistant remembering this?" and trace a memory back to the conversation it was written in.
