---
"server": minor
---

feat: require a user_session_issuer for tunneled and private-remote MCP servers. Enforced by two `mcp_servers` CHECK constraints (added NOT VALID, then validated) plus auto-provisioning on create/update, so no path can persist a tunneled or private-remote server without an issuer. Private remote servers are therefore always issuer-gated at serve time.
