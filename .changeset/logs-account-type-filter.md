---
"dashboard": patch
---

Add a team vs personal account type filter to the Tool Logs (/logs) page. Selecting an account type scopes the log traces via the raw-logs path (filtering on the materialized `gram.account_type` dimension); "Team" includes unclassified traces so they aren't dropped.
