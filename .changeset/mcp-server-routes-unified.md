---
"dashboard": patch
---

MCP server pages now live at `/mcp/<server>/<tab>`. Older links keep working: `/mcp/x/<server>/...` redirects to the new address, and `/mcp/<toolset>/...` opens the page of the server that hosts the toolset. When a server and a toolset share a slug, the server wins.
