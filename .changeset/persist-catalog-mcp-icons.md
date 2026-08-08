---
"server": minor
"dashboard": minor
---

Persist catalog MCP icons on mcp_metadata and render them in the dashboard. A new `assets.fetchImageFromURL` endpoint downloads a catalog server's registry icon into an image asset at install time, and the install workflow stores it as the server's MCP metadata logo. The MCP server detail sidebar now renders the persisted logo, and collection listings populate `icon_url` from it for both toolset-backed and mcp_server-backed servers.
