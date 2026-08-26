---
"server": patch
---

Platform MCP now finishes MCP setup over MCP. `get_mcp_client_admission` reports which MCP clients may authorize against a registered server — the effective Client ID Metadata Document admission mode plus that server's custom document URLs — and `set_mcp_client_admission` changes the mode after explicit user confirmation, writing the same audit event the dashboard's Authentication settings write. `attach_platform_mcp_identity_provider` is admitted to managed project assistants as well, so the remaining setup steps no longer require a detour into the dashboard.
