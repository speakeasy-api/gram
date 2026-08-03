---
"server": patch
---

Admit `chatgpt:usage` rows (ChatGPT/Work usage+spend from the OpenAI compliance COSTS import, previously retained but unread) into the agent-usage predicates of `attribute_metrics_summaries_mv` and `chat_session_summaries_mv`, via atomic MODIFY QUERY migrations. ChatGPT tokens now count toward tokens-under-management and appear in usage/cost analytics going forward, matching how Claude Chat (Anthropic Admin Analytics) and Cursor (Admin API) polled usage already bill. Applies to new rows only — previously parked rows are retained but not backfilled into the summaries. Also updates the stale MV comments that claimed no new `codex:usage` rows are written (the compliance import writes them, cost-only since the token double-count fix).
