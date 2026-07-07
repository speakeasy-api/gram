---
"dashboard": patch
"server": patch
---

Attach MCP servers to the Default plugin when they're enabled, not just when their first endpoint is created — remote MCP servers are created disabled with a pre-staged endpoint, so they previously never auto-attached and manually adding them failed with "mcp server is disabled or has no published endpoint". Also fixes creating a second endpoint for an already-attached server (previously failed on a duplicate-attach conflict), hides endpointless servers from the plugin's add-server picker, and asks for confirmation before removing a server's last address.
