---
"server": minor
---

Platform MCP can now set a project's Shadow MCP default access and manage a Shadow MCP server's official server link. `set_shadow_mcp_default_access` switches a project between allowed and blocked by default, and can narrow blocked by default to chosen roles. It refuses when a Guardrails or Shadow AI policy holds the setting. `set_shadow_mcp_official_server` links a remote Shadow MCP server to a project MCP server, switches the link off or on, or removes it. Both preview the change before it is confirmed, need an expected version and an idempotency key, and share the dashboard's write paths and audit log. `list_shadow_mcp_inventory` and `get_shadow_mcp_review` now report the default access and each server's link.
