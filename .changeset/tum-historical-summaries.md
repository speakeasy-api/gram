---
"server": minor
---

Tokens under management is now computed from the new `chat_token_summaries` ClickHouse aggregate instead of raw `telemetry_logs`. The summary table buckets token usage and stored-session evidence per chat per UTC day and is retained for 2 years, so TUM remains accurate across full billing cycles and historical cycles stay computable after the 30-day raw telemetry TTL expires. A backfill script captures the raw data still within the TTL window.
