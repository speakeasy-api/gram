---
"dashboard": minor
---

Surface the MCP catalog as a tab on the MCP page, next to MCP Servers, Sources and Deployments. The catalog keeps the same Add-from-catalog flow and now lives at `/mcp/catalog` rather than inside the add flow; `/catalog`, `/sources/add-from-catalog` and `/mcp/add/catalog` all redirect there, and "From the catalog" on the Add MCP server page still leads to it.
