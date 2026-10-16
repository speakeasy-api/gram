---
"server": patch
---

The organization MCP gateway now lists only servers you can actually use: servers that need no sign-in, servers already configured with credentials, and servers you can connect from the gateway's sign-in page. Servers whose authentication the gateway cannot provide no longer appear on the consent screen or in `list_servers`. Servers built on a toolset that has its legacy MCP endpoint turned off no longer fail with "not currently servable" when described or called through the gateway.
