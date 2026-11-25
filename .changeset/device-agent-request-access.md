---
"server": minor
---

Members can now ask for temporary access from the device agent. `agent.listRequestableAccess` lists the temporary permissions the signed-in member can still ask for, with the projects they can ask for each on, plus their own pending requests. `agent.requestAccess` files one, with an optional message and either a duration (1, 2 or 4 hours) or a deadline such as the end of their day, and notifies administrators the same way as a request from the dashboard. When a delivered MCP server says the member has no access, `agent.getMyConnections` now names the exact access to ask for, so the device agent can request it in one step. Both endpoints accept only a per-user device agent key and are off unless access requests are enabled for the organization.
