---
"dashboard": patch
"server": patch
---

Agents running on an API key no longer stay stuck on their old gateway after an edit: servers added in Edit now reach the keys they run with, as they already did for workload identity agents. The new `keys.extendAgentKeys` endpoint adds `mcp:connect` grants to the caller's own live keys for an agent. Each key's new policy is checked like issuance, against the agent's, its owner's and the caller's permissions, and the change is recorded in the audit log.
