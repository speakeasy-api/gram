---
"server": minor
"dashboard": minor
---

A remote MCP server's Team Access tab now shows the scopes its sign-ins request from each identity provider and where they come from. Read access on the server is enough to see the card; pinning still needs edit access on every server sharing the URL. `remoteMcp.getServerScopes` now reports each client's `issuer_name` and `issuer_url` and whether the caller `can_pin`.
