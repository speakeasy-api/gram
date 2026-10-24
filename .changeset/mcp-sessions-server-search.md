---
"server": patch
"dashboard": patch
---

Each gateway now has a Debug page, reached from its left navigation and from Command K, listing the sessions issued for that gateway with a server-side search that stays in the page URL. Search matches the person, agent, API key, workload, client, MCP server, and upstream provider each session shows; agent names match only for agents you can view, and `%` and `_` are matched literally. Sessions now record the MCP server, gateway or toolset they were issued for, and refreshes keep it, so a gateway's list stays accurate when gateways share a sign-in server or a gateway changes its sign-in server. Sessions whose origin was not recorded are left out unless their consent selection names the gateway. `userSessions.list` and the `platform_list_user_sessions` tool accept the same `search` and `meta_mcp_server_id` parameters.
