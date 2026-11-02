---
"dashboard": patch
"server": patch
---

Whether an MCP server's upstream needs sign-in now follows from its credentials: it does while at least one credential is turned on, and it does not once every credential is turned off or removed. The server's upstream credentials settings show this status instead of a "Needs credentials / Needs none" choice, and they flag setup as incomplete when the upstream asks for sign-in but no credential is turned on. Credentials can always be added. The `setMcpServerUpstreamAuthRequirement` endpoint is deprecated and no longer changes anything.
