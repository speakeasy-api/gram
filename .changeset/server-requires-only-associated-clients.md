---
"server": patch
"dashboard": patch
---

An MCP server on a shared user session issuer now uses only the upstream OAuth clients it is associated with. A bound client that no server is associated with is no longer required for, or resolved for, every server on that issuer. Detaching a client the server is not associated with now returns not found.
