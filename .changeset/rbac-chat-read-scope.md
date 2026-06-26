---
"server": minor
"dashboard": minor
"@gram/client": patch
---

Add a `chat:read` RBAC scope that gates access to agent session transcripts. The `chat.load` endpoint and the dashboard agent-sessions list are now scoped by `chat:read`: members can read sessions they own (a self-scoped grant carrying their `user_id` is injected per request), while admins hold an unrestricted grant and can read every session. Each dashboard session open is recorded in the audit log as a `chat_session:access` event. The scope is selectable in the role editor (Agent Sessions group) and the dev RBAC override toolbar.
