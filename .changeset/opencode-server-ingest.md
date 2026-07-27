---
"server": minor
---

Ingest opencode observability events natively. The hook ingest pipeline recognizes the `opencode` source (`parseOpencodeHookEvent`), giving opencode events native event-name fidelity instead of a generic fallback, and counts opencode tool calls in the telemetry summaries. Per-turn token/cost usage rows are wired through the summaries but stay empty until an upstream agenthooks release forwards OpenCode usage on turn-end.
