---
"server": minor
---

Add the `orgMemory` API that the dashboard uses to browse a project's Organizational Memory: an overview of the last 30 days, day, week and month digests, the sessions and memos each digest cites, and recent activity that hasn't been rolled up yet. It requires an unrestricted `chat:read` grant on the project and the `org-memory` rollout, and every read is audited.
