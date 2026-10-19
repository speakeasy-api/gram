---
"server": minor
"dashboard": patch
---

MCP servers returned by `mcpServers.get`, `mcpServers.list`, `mcpServers.listForOrg`, `mcpServers.create`, `mcpServers.update` and `remoteMcp.createServerAndMcpServer` now carry a `toolset` summary for hosted servers (id, slug, name, configured tool count excluding external MCP proxy tools, tool selection mode, external MCP slugs, whether the server is proxy-only, external OAuth server, and registry specifier), the `primary_endpoint_id` and `primary_endpoint_url` of the address that stands for the server, and the `platform_endpoint_slug` of its oldest platform endpoint. Custom-domain endpoints count only on a verified, activated domain. List endpoints load these in two batched queries regardless of page size, and `listForOrg` fills them only for servers the caller can read. If these details cannot be loaded, reads still succeed without them.
