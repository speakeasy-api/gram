---
"server": minor
---

feat: require a user_session_issuer for tunneled and private-remote MCP servers. Enforced by two `mcp_servers` CHECK constraints plus auto-provisioning on create/update, so no path can persist a tunneled or private-remote server without an issuer. Also drops the unused `classification` column from `user_session_issuers`.
