---
"server": patch
"admin": patch
---

Staff can start Organizational Memory eval runs against the seeded demo corpus and list or read their results through a new admin API. Runs go one at a time, are capped per rolling 24 hours, and a run left unresolved past any workflow's lifetime no longer blocks later starts.
