---
"server": patch
---

MCP server and Platform MCP logins now reconcile organization memberships from WorkOS before checking access, the same way dashboard login does, so a valid member no longer needs a separate dashboard login to connect. Membership additions from WorkOS also drop the cached organization list immediately.
