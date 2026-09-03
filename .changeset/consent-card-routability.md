---
"server": patch
---

The MCP connect page no longer shows a service as "Connected" when the stored credential would not be forwarded to that server (it was minted for another upstream, or before upstream resources were recorded); such a service now asks for a reconnect. A gateway call that finds no routable credential says so instead of reporting a rejected token.
