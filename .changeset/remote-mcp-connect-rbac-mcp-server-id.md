---
"server": patch
---

Fix the per-tool `mcp:connect` RBAC checks in the remote MCP proxy to use the `mcp_servers` id instead of the `remote_mcp_servers` id, so they resolve grants against the same resource as the server-level check and the toolset path.
