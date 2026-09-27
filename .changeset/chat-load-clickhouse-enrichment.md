---
"server": patch
---

Speed up chat.load on Agent Sessions by reading token/cost metrics from chat_session_summaries, skipping Claude OTEL scans for non-Claude chats, and running remaining ClickHouse enrichment in parallel.
