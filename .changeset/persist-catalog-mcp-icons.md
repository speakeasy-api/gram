---
"server": minor
"dashboard": minor
---

Persist catalog MCP icons on mcp_metadata and render them in the dashboard. A new `assets.fetchImageFromURL` endpoint downloads a catalog server's registry icon into an image asset at install time, and the install workflow stores it as the server's MCP metadata logo. When the registry entry has no icon, the install falls back to icons the server itself advertises in its initialize response (via the new `remoteMcp.discoverServerIcons` probe) and then to the server origin's favicon (ICO accepted on the URL-fetch path). The dashboard renders the persisted logo on MCP server cards, table rows, and the server-detail sidebar, and the Branding settings section gains a logo upload field that persists on Save. Collection listings now populate `icon_url` from the persisted logo for both toolset-backed and mcp_server-backed servers. Public MCP servers also advertise the persisted logo to MCP clients: the initialize response now carries the toolset name, title, website URL, and SEP-973 icons, and custom domains serve the logo at /favicon.ico for clients that resolve icons from the domain favicon.
