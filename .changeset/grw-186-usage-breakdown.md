---
"server": patch
---

Platform MCP gains `get_tool_usage_summary`, which breaks one project's tool calls down by what they reached over a named window: hosted MCP servers, tunneled MCP servers, gateways, shadow MCP servers, local tools, and skills. It reads the same target-aware pipeline as the dashboard Insights board and the managed `platform_get_tool_usage_summary` tool, so every surface reports one number for one window, and folds the names calling apps used for a configured server onto that server (with its `mcp_id`) so a hosted server reached through a plugin prefix is never counted as shadow MCP. Every target type is reported, zero included, with its share of all calls; shadow MCP servers are counted but not named.
