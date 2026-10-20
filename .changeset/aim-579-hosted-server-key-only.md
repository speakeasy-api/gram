---
"server": patch
"dashboard": patch
---

Hosted MCP plugin entries, install page metadata, and assistant attachments are now always written under the toolset's canonical MCP server, and the `hosted-mcp-server-key-writes` rollout flag is removed. That includes a canonical server that is disabled, and toolsets with MCP off or no MCP slug that have one. The `plugin:server_add` audit event for a hosted toolset names the MCP server. Adding a toolset that has no MCP server to a plugin, saving its install page metadata, or attaching it to an assistant now fails with "toolset has no hosted MCP server", and the Default plugin attach skips it. The Plugins page disables such toolsets in the server picker. Turning MCP on for a toolset now delivers its MCP server to matching role plugins, as enabling any other MCP server does; a removal recorded under either key still keeps it out. Readers still accept rows under either key. A toolset no longer reads install page metadata or headers stored under a server that holds its id but is not its canonical MCP server.
