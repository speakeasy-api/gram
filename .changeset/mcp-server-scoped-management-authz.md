---
"server": patch
---

Key MCP server management authorization on the server's grant resource id instead of the project id, aligning it with the toolsets surface and the serving path. Get, update, delete, and tool-filter reads now check `mcp:read`/`mcp:write` against the toolset id (toolset-backed) or mcp_servers row id (remote/tunneled), and listing filters to the servers the caller holds a grant for — so role grants scoped to a single server now unlock managing exactly that server. Project-wide (`project_id` dimension) and wildcard grants behave as before. Listing with no matching grants now returns an empty list instead of forbidden, and get/update/delete on a server the caller lacks now return forbidden after the project-scoped row lookup (404 for rows that don't exist in the project).
