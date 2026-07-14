---
"dashboard": minor
---

The Roles & Permissions "Specific Servers" picker for `mcp:connect` now lists remote and tunneled MCP servers alongside toolset-backed ones, storing the id each backend's enforcement actually checks (mcp_servers id for remote/tunneled, toolset id otherwise). The challenge view resolves mcp_servers ids to server names, and the "Specific tools" panel explains that remote/tunneled servers resolve tools dynamically and cannot be tool-permissioned.
