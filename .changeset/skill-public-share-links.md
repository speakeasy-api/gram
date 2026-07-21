---
"server": minor
---

Add public share links for skills. New management endpoints `skills.share` and `skills.unshare` mint and revoke an unguessable share token per skill, and the unauthenticated `skills.getShared` endpoint serves a redacted public view (name, display name, summary, latest content) by token. Archiving a skill revokes its active share link, share and revoke events are audited, and skill list/get responses surface the active `share_token`.
