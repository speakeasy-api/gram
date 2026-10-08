---
"server": patch
---

Platform MCP: adding a remote MCP server by URL or putting it into a plugin now consults the project's enabled Shadow MCP block policy instead of hidden rollout flags. When the policy refuses the server, the tool files a review request on the administrator's behalf and returns the review page, the policies in force, and a plain explanation; nothing is added or shared until the review is approved. The `platform-mcp-shadow-audience-enforcement` and `platform-mcp-direct-remote-distribution-disabled` feature flags are removed.
