---
"server": patch
---

Employees list linked accounts now attach by directory ownership (summary email resolved to the org user, or the account's own email) instead of by the raw telemetry user_ids folded into a summary. Stray telemetry rows that pair one person's email with another person's user id could previously hand an account — and the role bucket in the by-role view — to the wrong employee (DNO-509).
