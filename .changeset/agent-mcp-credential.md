---
"server": minor
---

Agent identities can mint their own MCP credential. `agent.mintMcpCredential`, called with an agent's enrollment key, returns a separate key for the same agent carrying only its MCP access: the agent's live `mcp:connect` grants and exclusions, bounded by its owner and by the person who authorized the enrollment key. Enrollment keys themselves need no `mcp:connect`. The device identifies itself with the `Gram-Device-Serial` or `Gram-Device-Hostname` header, and a mint that sends neither is rejected. Minting again from the same device revokes that device's previous credential, while other devices sharing the enrollment key keep theirs. The credential expires in 90 days by default, never after its parent, and revoking or expiring the enrollment key invalidates it.
