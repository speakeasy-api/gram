---
"dashboard": patch
---

The identity chaining control on an MCP server's authentication settings now selects the client the binding actually uses. Picking a different client shows "Use this client instead" rather than silently replacing the bound client through "Update scopes". The Enable, Update scopes, Disable and grant confirmation controls now require `project:write`, matching the API.
