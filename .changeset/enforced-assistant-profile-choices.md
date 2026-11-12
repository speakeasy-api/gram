---
"server": patch
---

For organizations enforcing typed auth clients, an assistant's credential for each MCP server is now chosen from the auth clients the server offers, so a server offering one usable client no longer waits for an administrator to pick it. An assistant turn whose credential choices cannot be loaded now fails and is retried instead of running with every connected server unavailable.
