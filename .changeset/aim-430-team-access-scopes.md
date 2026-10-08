---
"server": minor
"dashboard": minor
---

`remoteMcp.getServerScopes` now needs only read access on the target MCP server and reports each client's `issuer_name` and `issuer_url` and whether the caller `can_pin`. In the dashboard, the pinned scopes picker is read-only unless you have edit access on every server sharing the URL, and Settings › Identity shows a read-only "Requested at sign-in" summary of the scopes the connected client requests to anyone who can read the server.
