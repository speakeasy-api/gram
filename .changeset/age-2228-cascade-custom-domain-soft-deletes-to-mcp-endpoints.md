---
"server": minor
"dashboard": patch
---

Deleting a custom domain now soft-deletes every `mcp_endpoints` row registered under it across all projects in the org, emits one `mcp-endpoint:delete` audit event per cascaded row, and the dashboard delete-confirmation modal previews the impacted endpoints via the new `/rpc/domain.listMcpEndpoints` endpoint.
