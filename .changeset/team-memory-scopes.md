---
"server": patch
"dashboard": patch
---

Add the `memory:read` and `memory:write` permissions for upcoming team memory in the Platform MCP. `memory:read` lets someone search and recall a project's team memory, and `memory:write` lets them save memos to it. Neither is granted by any built-in role, including admin, so grant them per project through a custom role.
