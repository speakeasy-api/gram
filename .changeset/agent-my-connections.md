---
"server": minor
---

The device agent can now show which MCP servers a person still needs to sign in to. `agent.getMyConnections` reports the enrolled user's sign-in state for each MCP server in their plugins, with a link that signs in to it in the browser, and each server listed by `agent.getPlugins` now carries a stable `id` shared across plugins.
