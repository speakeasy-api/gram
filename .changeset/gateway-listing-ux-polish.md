---
"dashboard": patch
---

Polish the gateway UX on the MCP listing and detail pages: gateway cards show their member servers' logos and a status dot consistent with server rows, the listing copy and table columns account for gateways, member management (reorder, add, remove) moves onto the Overview tab and the separate Members tab is removed (its URL redirects to overview), detail-page tab switches scroll back to the top, the gateway sidebar URL truncates instead of wrapping mid-token, absent MCP metadata no longer replays 404 requests on every remount, and team-access rows (gateways and MCP servers alike) click through to the Access page's pre-filled grant dialog — "No access" cells deep-link the specific missing scope. Clickable table rows across the dashboard now highlight more strongly on hover.
