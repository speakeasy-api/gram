---
"server": minor
---

feat: require a user_session_issuer for every remote and tunneled MCP server. The server mints the issuer in the same transaction as the mcp_servers row and it lasts for the server's lifetime: `user_session_issuer_id` is removed from both the create and update APIs, and the update query COALESCEs to the stored value, so no code path can supply, strip, or swap it. Enforced at the schema level by a `mcp_servers` CHECK constraint (added NOT VALID, then validated). Toolset-backed servers are exempt (their issuer lives on the toolset).
