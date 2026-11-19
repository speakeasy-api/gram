---
"server": patch
---

Tool Logs filtered by user email, user id or external user id load much faster. These filters are now answered from the per-trace summaries instead of a scan of every raw log row, which could time out.
