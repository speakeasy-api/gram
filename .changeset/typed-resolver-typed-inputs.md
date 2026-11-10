---
"server": patch
---

With upstream auth enforcement on, an MCP server that needs no upstream authentication keeps working after its inherited sign-in offer is removed, instead of asking people to reconnect. New sessions also pick up a sign-in the server offers even when it was never attached to the server the older way.
