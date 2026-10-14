---
"server": patch
"dashboard": patch
---

Plugin server entries and MCP install page metadata for a hosted MCP server now return both `toolset_id` and `mcp_server_id`, set to the same id, whether the record is stored under the toolset or its MCP server. Callers that read `toolset_id` keep working once hosted records move to the MCP server key. Remote, gateway and other non-hosted entries are unchanged. Platform MCP plugin reads keep the toolset's MCP slug for a hosted entry stored under its MCP server when the server has no endpoint. `addPluginServer` accepts an entry echoed from a read, with `toolset_id` and `mcp_server_id` both set to the same id. The Plugins page's server filter matches a hosted server whichever id a saved filter names.
