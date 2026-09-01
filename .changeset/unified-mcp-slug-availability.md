---
"server": patch
"dashboard": patch
---

Unify MCP slug availability across toolsets and mcp_endpoints into a single namespace per address scope. A shared check (spanning both `toolsets.mcp_slug` and `mcp_endpoints.slug`) now backs `toolsets.checkMCPSlugAvailability`, `mcpEndpoints.checkMcpEndpointSlugAvailability`, toolset MCP slug updates, and MCP endpoint create/update, so an endpoint can no longer be created with a slug a live hosted (toolset-backed) server still resolves under, and vice versa. Owner exclusions let a hosted server's mirrored address validate against itself. The dashboard endpoint-slug validation hook drops its second RPC now that the endpoint check covers both tables.
