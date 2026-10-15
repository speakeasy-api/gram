---
"server": minor
"dashboard": minor
---

Adds the project-scoped `log:read` RBAC scope, which now gates a project's individual tool calls and raw log records: Tool Logs, the Playground logs panel, a session's Tool calls view, the underlying telemetry endpoints, and the matching Platform MCP and assistant tools. Like `chat:read`, no system role holds it and neither `project:*` nor `org:admin` implies it, so organization admins grant it per project through a custom role, including to themselves to keep using Tool Logs.
