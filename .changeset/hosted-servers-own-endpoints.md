---
"server": minor
---

Hosted MCP servers now own their hosting state and addresses. This is an API change: `toolsets.update` rejects `mcp_enabled`, `mcp_slug`, `mcp_is_public`, `network_access_mode` and `custom_domain_id` with a bad request error, and `toolsets.setUserSessionIssuer` and `toolsets.checkMCPSlugAvailability` are removed. Use `mcpServers.update` for visibility, network access and the session issuer, and `mcpEndpoints` to create, change and delete addresses; a hosted server can now have several endpoints. New toolsets get their MCP server and a platform endpoint at creation. Existing `/mcp/<slug>` addresses keep working.
