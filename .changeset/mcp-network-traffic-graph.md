---
"server": minor
"dashboard": minor
---

Show observed public versus private traffic in an MCP server's or gateway's Network access panel, so admins can check which route clients still use before switching to private only or back to public.

Each resolved inbound MCP request to a hosted, remote, tunneled or stored gateway endpoint now writes one `mcp_network_request` telemetry log carrying the server id and network surface. Rows carry no tool URN, so they never count as tool calls. A new `mcp_network_traffic_hourly_summaries` table keeps hourly totals for 90 days, and `telemetry.getMcpNetworkTraffic` returns zero-filled hourly points for a 24h or 7d window plus the last time each route was seen. Counts only cover requests observed while telemetry logs are enabled, and the panel says so.
