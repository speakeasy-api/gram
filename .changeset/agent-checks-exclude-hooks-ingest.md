---
"server": patch
"dashboard": patch
---

`listChallenges` takes `exclude_scopes`, applied before the total and the page. An agent's authorization checks leave out its org:hooks_ingest checks, one per hook event, so the checks that say what the agent may reach are no longer buried. The org Roles & Permissions checks page is unchanged.
