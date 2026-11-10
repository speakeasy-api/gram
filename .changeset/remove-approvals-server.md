---
"server": minor
"dashboard": minor
---

Remove the built-in "Speakeasy Approvals" MCP server. Saving an approval policy no longer creates a per-project approvals server or adds it to the Default plugin, and policy responses no longer carry `approvals_server_setup`. Held tool calls now always point agents at the shared `/approvals-mcp` route, which accepts keys. Existing approvals servers answer as not found and drop out of plugins on their next publish. The MCP, plugin, gateway and policy pages no longer have Built-in server handling, and `builtin_backend` is gone from the MCP server API.
