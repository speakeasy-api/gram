---
"server": minor
---

Ingest opencode observability events natively. The hook ingest pipeline recognizes the `opencode` source (`parseOpencodeHookEvent`), giving opencode events native event-name fidelity instead of a generic fallback, and counts opencode tool calls in the telemetry summaries. Per-turn token/cost usage rows are populated from the OpenCode turn-end usage forwarded by agenthooks v0.4.0.
