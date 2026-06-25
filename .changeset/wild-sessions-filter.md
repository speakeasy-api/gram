---
"server": patch
---

fix(telemetry): match `listSessions` dimension filters per-chat instead of per-row so combining a user-directory filter (e.g. department) with `hook_source` no longer returns empty when those attributes live on different rows of the same chat
