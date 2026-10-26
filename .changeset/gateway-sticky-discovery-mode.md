---
"server": patch
"dashboard": patch
---

A gateway connection approved with "Gateway default" now keeps the discovery mode that was the default at consent until it reconnects, so changing a gateway's default mode (for example to Code Mode) no longer swaps the tools of connected clients mid-session. A tool call for a tool the connection's mode does not serve now says the gateway's tools may have changed and to reconnect, instead of "unknown code mode tool".
