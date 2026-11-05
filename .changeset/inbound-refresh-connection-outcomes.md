---
"server": patch
---

Record inbound MCP refresh outcomes, including successful rotation, replay, expired or refused grants, and upstream reconnect requirements. Outages during refresh checks now return a retryable 503 or 500 instead of `invalid_grant`.
