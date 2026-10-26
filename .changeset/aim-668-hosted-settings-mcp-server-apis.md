---
"dashboard": patch
---

Hosted MCP server settings now save the slug, custom domain, network access and the linked sign-in issuer through the MCP server and endpoint APIs instead of the deprecated toolset hosting endpoints, and slug checks use the endpoint slug availability check. Toolsets without a hosted MCP server still link their issuer through the toolset. After a save that changes the server's slug, the page now moves to the new address instead of showing "not found".
