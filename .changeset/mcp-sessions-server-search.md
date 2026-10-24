---
"server": patch
"dashboard": patch
---

MCP Sessions search now runs on the server across every connection, not only the rows already loaded, and the search stays in the page URL. It matches the person, agent, API key, workload, client, MCP server, and upstream provider each connection shows. Agent names match only for agents you can view, and `%` and `_` are matched literally. `userSessions.list` and the `platform_list_user_sessions` tool accept the same `search` parameter.
