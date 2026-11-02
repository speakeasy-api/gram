---
"server": patch
---

An agent API key calling an MCP server's own endpoint now gets the same credential checks as through its agent gateway. Before, a key without a credential choice for the server was served by the agent's attached personal connection even when the agent was blocked from that credential or connection, or lacked permission to use it.
