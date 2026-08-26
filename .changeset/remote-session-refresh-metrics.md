---
"server": minor
---

New `gram.remote_session.upstream_refresh` and `mcp.request.rejected` metrics count every upstream remote session refresh attempt by outcome and trigger, and every MCP request the Session OAuth authentication gate rejects by reason, server, and surface. The matching failure logs now carry the issuer URL, outcome, user id, MCP slug, and URL needed to attribute them.
