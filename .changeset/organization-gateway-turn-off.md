---
"server": minor
---

Organization admins can turn the organization gateway off with the `organization_gateway_disabled` product feature. While it is off, `/mcp/gateway/<organization-slug>` and its sign-in routes answer 404 from the next request, `organizations.getGateway` reports `turned_off`, and the Platform MCP's `get_my_organization_gateway` tells members to ask an admin to turn it back on. The gateway stays on for organizations that never change the setting.
