---
"server": minor
"dashboard": minor
---

Every organization with one organization-wide authorization server now has an organization gateway at `/mcp/gateway/<organization-slug>`: one MCP URL that gives each person every MCP server in the organization they can use, across all projects. People sign in with their organization account, and the servers they see follow their access, so a granted or revoked server shows up or disappears on their next request. Servers keep their own sign-ins: the gateway's connect page lists the services behind servers in any project, and a server whose service is not connected yet reports that on its own while the rest keep working.

The MCP Servers page shows a Connect all servers button with the URL when the gateway is available, `organizations.getGateway` reports the URL or why there is none, and the Platform MCP's new `get_my_organization_gateway` tool gives it to members. The delete preflight for an organization-wide authorization server now reports `withdraws_organization_gateway` when deleting it would take the gateway away.
