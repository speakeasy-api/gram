---
"server": minor
---

The device agent shares the hidden MCP server preference with the gateway consent page. `agent.getMyConnections` reports each server's `hidden` state, and the new `agent.hideMcpServer` and `agent.unhideMcpServer` set it, so hiding a server in one place hides it in the other.
