---
"dashboard": minor
---

Surface the MCP catalog as a tab on the MCP page, next to MCP Servers, Sources and Deployments. The catalog keeps the same Add-from-catalog flow and now lives at `/mcp/catalog` rather than inside the add flow; `/catalog`, `/sources/add-from-catalog` and `/mcp/add/catalog` all redirect there, and "From the catalog" on the Add MCP server page still leads to it.

Catalog entries are also searchable from the command palette, in an "MCP Catalog" group kept separate from the "MCP Servers" group so a third-party server you could add is never mistaken for one the project already runs. The group appears once you start typing, and selecting an entry opens its catalog page.
