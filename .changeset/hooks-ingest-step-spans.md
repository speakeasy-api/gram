---
"server": patch
---

Hook ingestion now records a trace span for each gating and persistence step (quarantine and spend gates, risk scan, warn acknowledgement, shadow-MCP guard, idempotency claim, skill activation, event persistence, MCP inventory caching), so slow gating requests can be traced to the step that stalled instead of showing as an unexplained gap under `hooks.ingest`.
