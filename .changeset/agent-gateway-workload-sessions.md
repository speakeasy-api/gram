---
"server": patch
---

Agents can now use a stored MCP gateway. A workload signed in with workload identity can exchange its platform token for a session naming the gateway URL, and a session minted for all of an issuer's MCP servers now works at that issuer's gateways too. Agent keys and principal credentials work there as well. Each request lists and calls only the gateway members the agent and its owner may both connect to, public members included, checked again on every request, and a session minted for a gateway works only at that gateway. A member that signs in upstream is called with the credential the agent would use at that member's own server: for a workload, its agent owner's own connection.
