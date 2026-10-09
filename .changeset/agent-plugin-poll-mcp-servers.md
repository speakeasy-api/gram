---
"server": minor
"dashboard": patch
---

Device agents polling with an agent API key now receive `mcp_servers`: the Speakeasy-hosted MCP servers in the plugins assigned to the agent, which the device writes into each tool's configuration with its own credential. Servers in plugins offered as available, unproxied vendor servers and gateway members are left out. The list is always present and empty for people, and changes to it change the poll's ETag.
