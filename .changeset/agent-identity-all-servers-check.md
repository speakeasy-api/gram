---
"server": minor
"dashboard": patch
---

"All servers" in the new agent identity flow, and when editing an agent's servers, no longer ends in "requested agent access exceeds your permissions". The option is offered only when you, and the agent's owner, can reach every server in the agent's project. Otherwise it stays off and says why, and you choose the servers one by one. The new `agents.checkPolicyGrants` endpoint answers whether policy grants could be written, using the same rule as `agents.create` and `agents.createPolicyGrant`, and writes nothing.
