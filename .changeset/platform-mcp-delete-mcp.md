---
"server": minor
---

Add a `delete_mcp` Platform MCP tool so organization administrators can delete any MCP server in a project, including servers created in the dashboard or by other people. It runs the dashboard's own delete, so a remote or tunneled server is removed and a server hosted from a toolset is turned off, and every delete is audited. The caller names the exact project and server, passes the version from a fresh `find_mcp` or `get_mcp` read, an idempotency key, and `confirmed: true`. `find_mcp` and `get_mcp` now return that version for every server.
