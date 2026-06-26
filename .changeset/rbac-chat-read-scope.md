---
"server": minor
"dashboard": minor
---

Add a `chat:read` RBAC scope that gates access to other members' agent session transcripts. The `chat.load` endpoint and the dashboard agent-sessions list are scoped by `chat:read`: anyone can always read sessions they own (the handler grants owner access directly — no `chat:read` grant needed), while reading every member's session requires an unrestricted `chat:read`. The scope is not a default of any system role — not even `admin` — so it must be granted explicitly via a custom role. On the agent-sessions page, callers without `chat:read` see a banner noting they only see their own sessions (with a link to the roles page for org admins). Each dashboard session open is recorded in the audit log as a `chat_session:access` event. The scope is selectable in the role editor (Agent Sessions group) and the dev RBAC override toolbar.
