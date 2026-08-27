---
"dashboard": minor
"server": minor
---

Gateway Endpoints can now be created and managed from the dashboard, behind the `gram-gateway-endpoints` rollout flag. A gateway is one MCP endpoint fronting a curated set of MCP servers, so it appears in the MCP inventory alongside hosted and remote servers and gets its own detail page: Overview (canonical URL and member summary), Members (add, remove, reorder), Inspect, Clients and Sessions, and Settings (name, addresses including custom domains, authentication, delete). Creating one asks for a name and provisions a default address.

Inspect reads the endpoint live over MCP and shows exactly what a client receives — the tool surface, the instructions sent on connect, the current `list_servers` state, and a `describe_server` drill-down — rather than anything derived from dashboard state. It connects as the signed-in user, so `userSessions.mint` accepts a `meta_mcp_server_id` target alongside the existing toolset and MCP-server ones; that arm requires the same `mcp:connect` permission the runtime gate enforces, so a caller whose grant is restricted to other resources cannot mint for a gateway. Where the endpoint serves the caller fewer members than are configured, the tab says so and why. Member status likewise reports only what the backend attests: hosted members are available, proxied members read unknown until the gateway runtime holds live upstream sessions.
