---
"server": patch
"dashboard": patch
---

Show roles granted through directory role mappings everywhere a member's roles are read, not only in permission checks. The Team page, Roles page, role filters, audience and plugin reach, admin notifications, spend rules, Platform MCP member search, the MCP consent agent picker, telemetry, and billing now include them. Members now report mapped roles in a separate `directory_role_ids` field, and the Team page marks them as coming from the directory because they cannot be removed there.
