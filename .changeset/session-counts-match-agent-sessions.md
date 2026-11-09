---
"server": minor
"dashboard": patch
---

The project dashboard's Sessions tile and Most Agent Sessions by User card now count the sessions shown on the Agent Sessions page. They used to count session ids from telemetry, which includes sessions that were never captured, so the dashboard could show many more sessions than the list it links to. A new `chat.countByUser` endpoint returns per-user session counts with the same filters and visibility rules as `chat.list`.
