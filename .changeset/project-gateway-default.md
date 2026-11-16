---
"server": minor
"dashboard": minor
---

Each project's MCP Servers list now leads with its Project Gateway, the Organization Gateway limited to that project's servers, in place of the Organization Gateway. The Project Gateway page shows the project's endpoint, installation steps, connections, and activity to anyone with access to the project, and carries the switch that turns gateways on or off for organization members. The Organization Gateway page now points to Project Gateways and keeps only its endpoint and switch. `organizations.getProjectGateway` returns the selected project's gateway URL. `gatewaySessions.list` and `gatewaySessions.listFacets` accept `project_id` to list one project's gateway sessions, and `telemetry.getProjectGatewayOverview` and `telemetry.getProjectGatewayUsage` replace the organization-wide gateway activity endpoints. In the Platform MCP, `list_gateway_sessions` gains a `project` scope and `get_organization_gateway_activity` accepts `project_id`.
