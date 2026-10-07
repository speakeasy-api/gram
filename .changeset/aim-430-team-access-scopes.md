---
"server": minor
"dashboard": minor
---

A remote MCP server's Team Access tab now shows the scopes its sign-ins request from each identity provider and where they come from. `remoteMcp.getServerScopes` now needs read access to the server and to every server that shares its URL, and reports each client's `issuer_name` and `issuer_url` and whether the caller `can_pin`.
