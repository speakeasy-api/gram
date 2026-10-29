---
"server": patch
---

Agents can now use an MCP gateway. A workload signed in with workload identity can exchange its platform token for a session naming the gateway URL, and a session minted for all of an issuer's MCP servers now works at that issuer's gateways too. Agent keys work there as well. Each request lists and calls only the gateway members the agent and its owner may both connect to, checked again on every request. A member that signs in upstream still needs a credential the agent can use.
