---
"server": patch
---

An Enterprise Managed Auth sign-in to a gateway no longer fails when one of the gateway's servers isn't connected yet. The session is issued for the servers that are ready, and calling a server that isn't connected returns a link to its connect page. After connecting, the app picks the server up the next time it signs in to the gateway. Sign-ins to a single MCP server still fail until it is connected.
