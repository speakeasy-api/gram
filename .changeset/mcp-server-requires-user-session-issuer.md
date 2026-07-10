---
"server": minor
---

feat: require a user_session_issuer for every remote and tunneled MCP server. The issuer is supplied at create time (the create API rejects remote/tunneled servers without one) and is attached for the server's lifetime: `user_session_issuer_id` is removed from the update API and the update query COALESCEs to the stored value, so no code path can strip or swap it. Enforced at the schema level by a `mcp_servers` CHECK constraint (added NOT VALID, then validated). Toolset-backed servers are exempt (their issuer lives on the toolset).
