---
"server": patch
---

An agent API key calling an MCP server's own endpoint gets the same credential checks as through its agent gateway: a key without a credential choice for the server is refused when the agent is blocked from the credential or connection, or lacks permission to use it.
