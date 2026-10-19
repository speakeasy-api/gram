---
"server": patch
---

Attaching a toolset whose hosted MCP server is turned off to an assistant now requires `mcp:write` on that toolset, since attaching turns the server on. Toolsets whose MCP server is already on still only need `mcp:connect`. Toolsets already attached to the assistant are left as they are on update: they are not turned back on and need no extra permission.
