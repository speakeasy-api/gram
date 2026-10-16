---
"server": patch
"dashboard": patch
---

Use agent write permission for both creation and management, with an `owner` selector for each role holder (`self`), selected people (user IDs), or everyone (`*`). Preserve automatic management of already-owned agents and check delegated access against the agent's current owner. Existing unrestricted write grants continue to work without a backfill.
