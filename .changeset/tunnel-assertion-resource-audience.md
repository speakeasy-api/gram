---
"server": patch
"dashboard": patch
---

Use a tunneled server's saved resource identifier as the audience of signed caller assertions. When the setting is empty, use `tunneled-mcp-server:<ID>`. Preserve trailing slashes and escaped characters when saving the identifier, and explain the audience setting in the dashboard. Show the organization ID that caller assertions carry in `organization_id` on tunneled MCP server settings.
