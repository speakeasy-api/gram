---
"server": patch
---

The External MCP migration page no longer blocks servers that have no sign-in provider, whose sign-in provider has no upstream client, that use an OAuth proxy or an external OAuth server, that serve other tools, or whose users send their own headers. Those servers now migrate: an OAuth proxy, an external OAuth server and any other tools are dropped, and the page shows each one as a warning before the migration runs.
