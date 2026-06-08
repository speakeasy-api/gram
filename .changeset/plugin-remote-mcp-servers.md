---
"server": minor
"dashboard": minor
---

Support adding Remote MCP-backed `mcp_servers` to plugins alongside toolset-backed servers. `plugins.addPluginServer` accepts either `toolset_id` or `mcp_server_id` (exactly one), `PluginServer` exposes `mcp_server_id`, and `display_name` is now optional (defaulting to the backing toolset or mcp_server name). Plugin bundle generation resolves the preferred endpoint for mcp_server-backed servers (custom-domain over platform, then oldest) and emits them as OAuth HTTP servers with no static auth header. In the dashboard, the plugin add-server picker and server cards offer and render Remote MCP-backed servers (gated on the `gram-remote-mcp` feature flag).
