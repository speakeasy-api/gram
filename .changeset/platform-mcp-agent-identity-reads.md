---
"server": patch
---

Platform MCP can read an agent identity: `get_agent_mcp_access` lists the MCP servers its mcp:connect grants reach and whether it signs in with a shared credential or on behalf of named people, and `list_agent_authorization_checks` lists its recent authorization checks with denials first, leaving out org:hooks_ingest. Both use the dashboard's agent permissions.
