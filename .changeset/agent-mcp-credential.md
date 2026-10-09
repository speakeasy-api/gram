---
"server": minor
---

Agent identities can mint their own MCP credential. `agent.mintMcpCredential`, called with an agent's enrollment key, returns a separate key for the same agent that carries only the enrollment key's `mcp:connect` grants and expires in 90 days by default, never after its parent. Minting again revokes the previous credential, and revoking or expiring the enrollment key invalidates it.
