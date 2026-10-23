---
"server": minor
---

Hosted MCP servers now write their own hosting state, and the toolset's hosting fields mirror it. This is an API change: `toolsets.update` rejects `mcp_enabled`, `mcp_slug`, `mcp_is_public`, `network_access_mode` and `custom_domain_id` with a bad request error, and `toolsets.setUserSessionIssuer` and `toolsets.checkMCPSlugAvailability` are removed. Use `mcpServers.update` for visibility, network access and the session issuer, and `mcpEndpoints.update` to change a hosted server's address. A hosted server keeps a single address. New toolsets get their MCP server and a platform endpoint at creation.
