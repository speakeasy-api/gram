---
"server": patch
"dashboard": patch
---

Fix Claude Desktop agent sessions showing an opaque user ID instead of the user's name. The Anthropic compliance import no longer clobbers a previously resolved chat owner when a later sync activity carries no actor identity (empty strings defeated the upsert's COALESCE guard — NULL is passed instead), and connected-user email resolution is now case-insensitive on both the server and the dashboard. When a session's owner still can't be matched to an org member, the agent-sessions list and session details now show a tooltip explaining why.
