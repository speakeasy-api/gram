---
"server": minor
---

Platform MCP can now use your own MCP servers without a second connection. `list_my_servers`, `search_my_tools`, `describe_tools` and `execute_tool` reach the servers your organization gateway gives you, as you and with your own upstream sign-ins. `search_my_tools` finds the right tool across all of them in one call, and each connection's instructions name your servers so agents know to use them. They are behind the `platform-mcp-gateway-proxy` flag. For these tools the organization gateway counts as on unless an administrator turned it off.
