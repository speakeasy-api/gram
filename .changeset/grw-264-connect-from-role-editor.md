---
"dashboard": patch
---

The role editor's tool access sheet can now connect a remote MCP server itself. For a server with no stored tool list, an editor with `mcp:write` on it gets the tools listed through a live session; with no upstream session yet, **Connect** opens the server's connect page in a new tab, and coming back lists the tools and records them. An editor without `mcp:write` is told that setting tool-level permissions requires it, and no session is opened. The Inspect tab and the sheet share one connection hook.
