---
"server": patch
---

Let an agent create and rename projects through the Platform MCP. `create_project` makes an empty project from a display name, deriving its slug exactly as the dashboard does, and `rename_project` changes only a project's display name, keeping its slug. Both require organization administrator access, explicit confirmation and an idempotency key, so a retried creation returns the project the first call made instead of a second one. The dashboard and the Platform MCP now share one project creation path, so an agent-created project gets the same Default environment, Default plugin and audit entries, and a name with no letters or digits is refused as an invalid request instead of failing as a server error.
