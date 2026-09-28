---
"server": patch
---

Platform MCP diagnostics now attribute usage to the configured MCP server. `query_mcp_metrics` counts the calls the gateway proxied to a remote, tunneled, or gateway-member server instead of reporting zero beside a nonzero latency, reports active users for those servers, and never claims `no_observations` when any metric in the same result is nonzero. `get_project_overview` folds the names calling apps used for a server (a plugin-routed prefix, a bare slug, a display name, or an id) onto the configured server and returns its `mcp_id`, so `top_servers` line up with the ids the other tools take; names no configured server is known by are reported as the app used them.
