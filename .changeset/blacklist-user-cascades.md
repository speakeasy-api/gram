---
"server": patch
---

Cascade revocations atomically when a user is blacklisted: disable organizations where the user is the sole active non-staff member, soft-revoke API keys they created, revoke their sign-ins, user sessions, consents, Platform MCP connections and sessions, Channels OAuth grants, and sole-user OAuth clients, write audit logs, and invalidate the in-memory block snapshot after commit.
