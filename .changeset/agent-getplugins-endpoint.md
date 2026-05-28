---
"server": minor
---

Add `agent.getPlugins` management API method consumed by the Speakeasy device agent. The endpoint accepts an `email` query parameter, resolves plugin assignments for that email plus the `*` wildcard within the caller's org, and returns the assigned plugins with each MCP server's URL. Authenticates with an org-scoped API key carrying the new `agent` scope.
