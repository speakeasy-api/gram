---
"server": minor
"dashboard": minor
"@gram/client": patch
---

Add a `chat:read` RBAC scope that gates access to other members' agent session transcripts. The `chat.load` endpoint and the dashboard agent-sessions list are now scoped by `chat:read`: members can always read sessions they own (the handler grants owner access directly — no `chat:read` grant needed), while admins hold an unrestricted `chat:read` and can read every member's session. Each dashboard session open is recorded in the audit log as a `chat_session:access` event. The scope is selectable in the role editor (Agent Sessions group) and the dev RBAC override toolbar.
