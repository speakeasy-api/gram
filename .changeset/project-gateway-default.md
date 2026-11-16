---
"server": minor
"dashboard": minor
---

Each project's MCP Servers list now leads with its Project Gateway, the Organization Gateway limited to that project's servers, in place of the Organization Gateway. The Project Gateway page shows the project's endpoint, installation steps, connections, and activity, and carries the switch that turns gateways on or off. The Organization Gateway page now points to Project Gateways and keeps only its endpoint and switch. `gatewaySessions.list` and `gatewaySessions.listFacets` accept `project_id` to list one project's gateway sessions, and `telemetry.getProjectGatewayOverview` and `telemetry.getProjectGatewayUsage` replace the organization-wide gateway activity endpoints.
