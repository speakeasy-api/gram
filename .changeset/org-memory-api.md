---
"server": minor
---

Add the `orgMemory` API that the dashboard uses to browse a project's Organizational Memory: an overview of the last 30 days, day, week and month digests, the sessions and memos each digest cites, and recent activity that hasn't been rolled up yet. It requires `memory:read` on the project and the `org-memory` rollout, returns session titles only to callers who also hold `chat:read`, and every read is audited.
