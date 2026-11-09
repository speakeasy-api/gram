---
"server": patch
---

Platform MCP can read an agent identity: `get_agent_mcp_access` lists the MCP servers it may connect to, decided as its keys are checked (its own grants, its roles, and grants to everyone, where mcp:connect, mcp:read and mcp:write all count and blocks apply), the rules behind each, and whether it signs in with a shared credential, on behalf of named people, with no one assigned, or with the credential its own client sends. `list_agent_authorization_checks` lists its recent authorization checks with denials first, leaving out org:hooks_ingest. Both use the dashboard's agent permissions. Member access reads now also count a grant that names a toolset-backed server by its toolset ID, as the dashboard's Access page writes it.
