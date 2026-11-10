---
"server": patch
---

Drop the retired toolset MCP hosting columns `toolsets.mcp_slug`, `mcp_is_public`, `mcp_enabled`, `custom_domain_id` and `user_session_issuer_id`. Hosted MCP servers own this state.
