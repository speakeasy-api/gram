---
"server": minor
---

Agent identities can mint their own MCP credential. `agent.mintMcpCredential`, called with an agent's enrollment key, returns a separate key for the same agent carrying only its MCP access: the agent's live `mcp:connect` grants and exclusions, bounded by its owner and by the person who authorized the enrollment key. Enrollment keys themselves need no `mcp:connect`. The credential expires in 90 days by default, never after its parent. Minting again revokes the previous credential, and revoking or expiring the enrollment key invalidates it.
