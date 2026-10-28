---
"server": minor
---

A hosted MCP server can now have several endpoints. `mcpEndpoints.create` adds one under the hosted address rules, and `mcpEndpoints.delete` removes one; an enabled hosted server cannot lose its last endpoint that can serve, so disable it first. Deleting an endpoint left on a deleted or unverified domain is always allowed.
