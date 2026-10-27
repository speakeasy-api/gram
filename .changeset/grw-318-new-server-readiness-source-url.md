---
"server": patch
---

Platform MCP checks a newly added MCP server's readiness the first time you ask, instead of reporting it as temporarily unavailable until you force a check. `find_mcp` also finds a server by its source URL, even after the server is renamed.
