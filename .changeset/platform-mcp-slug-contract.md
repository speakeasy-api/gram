---
"server": minor
---

Platform MCP tools now identify MCP servers by `mcp_id` and projects by `project_slug`, as `list_projects` returns it, instead of accepting project IDs. This includes `list_gateway_sessions` project scope, `get_organization_gateway_activity`, `select_assistant_runtime` and `set_assistant_reasoning_effort`. Write tools that take a `confirmed` field preview on `confirmed: false`: data export pause, resume and level changes and risk policy audience changes run the same checks as the confirmed call and return a preview without writing. Tool descriptions and shipped Platform MCP skills were sharpened from tool-selection eval findings.
