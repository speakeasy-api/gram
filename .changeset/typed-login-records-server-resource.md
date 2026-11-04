---
"server": patch
---

Connecting an upstream account through an MCP server's auth client now records that server as the connection's resource. Previously a new connection could be saved with no resource, leaving the consent page stuck on "Connected elsewhere" with access blocked.
