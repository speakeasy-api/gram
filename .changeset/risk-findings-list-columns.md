---
"server": patch
---

Add `message_created_at` and `assistant_id` columns to the ClickHouse
`risk_findings` table and stamp them at ingest from the chat-message
attribution lookup. `message_created_at` (defaulting to scan time for
pre-existing rows) will let the Risk Events listing sort and paginate by
event time from ClickHouse; `assistant_id` will power the assistant filter
without a cross-store join.
