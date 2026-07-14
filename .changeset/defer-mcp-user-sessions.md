---
"server": patch
---

Stop persisting a user_sessions row when the dashboard mints a user-session JWT. Viewing an MCP server or the playground no longer surfaces as an active user session; sessions now only appear when a client establishes one via the real OAuth flow.
