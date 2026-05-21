---
"server": minor
---

Cron triggers now accept an optional `note` field, matching wake triggers. The note is included in every scheduled tick the assistant sees, letting one assistant carry multiple cron triggers with distinct per-schedule steering (e.g. "run daily digest" vs "check deploy status").
