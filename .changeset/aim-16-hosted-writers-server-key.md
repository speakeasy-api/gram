---
"server": patch
"dashboard": patch
---

Behind the `hosted-mcp-server-key-writes` rollout flag (off by default), hosted MCP plugin entries, install page metadata, and assistant attachments are saved under the toolset's canonical MCP server instead of the toolset. With the flag off, saves keep today's row shapes. Adding a hosted toolset to a plugin (directly, on toolset creation, or when MCP is turned back on) stores the entry under its MCP server. Saving install page metadata moves an existing toolset-keyed record onto the MCP server in place, keeping its id and environment configuration, and drops a duplicate MCP-server-keyed record beside it (the toolset-keyed record wins, as every reader already shows it). Saving an assistant moves its hosted attachments onto the MCP server, keeping their environments, so the assistant's runtime config is unchanged. Toolsets without a canonical MCP server keep the toolset key, and so do assistant attachments to a toolset with MCP off or no MCP slug. The `plugin:server_add` audit event for a wrapped toolset names the MCP server instead of the toolset. The Plugins page's server filter lists a hosted server once whichever key its entries use.
