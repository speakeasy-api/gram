---
"server": patch
---

User session issuers in shared mode serve one OAuth authorization server at `/oauth/usi/{id}` for all of their MCP servers. Clients name the MCP server they want with the `resource` parameter, and each access token is bound to that one server. The MCP servers' protected resource metadata names the shared authorization server, while their existing per-server authorization servers keep working. Issuers in the default per-endpoint mode are unchanged.
