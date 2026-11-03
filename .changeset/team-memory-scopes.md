---
"server": patch
"dashboard": patch
---

Add the `memory:read` and `memory:write` permissions for upcoming team memory in the Platform MCP. `memory:read` lets someone search and recall a project's team memory, and `memory:write` lets them save memos to it. Organization admins hold both by default. Grant them to members per project through a custom role, on the Access page or, for organization admins, with the new Platform MCP `configure_memory_access_role` tool.
