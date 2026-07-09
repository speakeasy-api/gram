---
"dashboard": patch
---

fix(insights): count employee "Agent Sessions" from telemetry instead of the Postgres chat list. The card previously counted chats matched by an email substring search, which only reflected sessions mirrored into Postgres via `session_capture` and under-reported real activity. It now uses `summary.totalChats` (distinct `chat_id`s in telemetry, keyed by the same user), so the count is consistent with every other metric on the employee detail page.
