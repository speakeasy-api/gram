---
"server": patch
---

Keep typed auth clients and credential choices synchronized when consolidating authorization servers, without requiring a separate upstream OAuth migration run. Existing explicit choices and permissions are preserved. When both authorization servers had a client for the same MCP server, existing sign-ins keep the client they were already using.
