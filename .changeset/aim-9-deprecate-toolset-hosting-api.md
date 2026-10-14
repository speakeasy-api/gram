---
"server": patch
"dashboard": patch
---

The toolset API's MCP hosting inputs are deprecated: `mcp_enabled`, `mcp_slug`, `mcp_is_public`, `network_access_mode` and `custom_domain_id` on `toolsets.update`, plus `toolsets.setUserSessionIssuer` and `toolsets.checkMCPSlugAvailability`. They still behave as before for this release. Use `mcpServers.update`, `mcpEndpoints.create`/`mcpEndpoints.update` and `mcpEndpoints.checkSlugAvailability` instead; note the endpoint check is scoped to one domain and returns true when the slug is available. The OpenAPI document and SDK mark them deprecated, and the server logs a warning with the caller's user agent when a request uses them.
