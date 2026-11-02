---
"dashboard": patch
"server": patch
---

Adding an MCP server to an agent in Edit now reaches the keys the agent already has, so its gateway lists the new server without issuing a new key. The new `keys.extendAgentKeys` endpoint adds `mcp:connect` grants to the caller's own live keys for an agent. Each key's new policy is checked like issuance, against the agent's, its owner's and the caller's permissions, and the change is recorded in the audit log.
