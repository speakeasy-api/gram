---
"server": patch
---

Shadow MCP admission no longer depends on hidden rollout flags: the project's enabled block policy is always enforced for user-supplied MCP servers, and an organization administrator's own write is recorded as the approval. When an administrator puts a server added by URL into a plugin, or widens a plugin's audience, from the Platform MCP or the dashboard, the server is recorded in Shadow MCP as approved for exactly the people who receive it, and the Platform MCP result reports that standing with a link to its page. The `platform-mcp-shadow-audience-enforcement` and `platform-mcp-direct-remote-distribution-disabled` feature flags are removed.
