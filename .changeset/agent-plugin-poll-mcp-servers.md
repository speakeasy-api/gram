---
"server": minor
"dashboard": patch
---

Device agents polling with an agent API key now receive `mcp_servers`: the Speakeasy-hosted MCP servers in the plugins assigned to the agent, which the device writes into each tool's configuration with its own credential under a `speakeasy-<slug>` name that stays stable across polls. Servers in plugins offered as available, unproxied vendor servers and gateway members are left out. Each entry also names the token endpoint, assertion audience and, where needed, resource a headless device exchanges its workload identity at to reach the server; entries no workload grant reaches carry none. The list is always present and empty for people, and changes to it change the poll's ETag.
