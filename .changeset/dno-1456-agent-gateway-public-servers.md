---
"server": patch
---

An agent's MCP gateway now lists and serves only the MCP servers the agent was granted. Public servers the agent was not granted no longer appear in `list_servers` and cannot be called through the gateway. The organization gateway and stored gateways still include public servers.
