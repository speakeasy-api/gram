---
"server": minor
---

`agent.getPlugins` now lists the MCP servers inside each plugin, so the Speakeasy Agent menu bar can show a server count on each plugin row and expand it to list the servers. A server includes a link to its dashboard page only when the enrolled user holds `mcp:read` on it. A plugin with no servers sends an empty list.
