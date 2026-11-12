---
"server": patch
"dashboard": patch
---

Agents now find a Code Mode gateway when a request names one of its servers or something only that server does. The `execute` tool description starts with the gateway's servers, how each server describes itself, and a sample of its tool names, and Claude Code loads the tool at session start instead of leaving it to tool search. Gateway instructions repeat each server's description after the line that names the servers. Server descriptions are recorded when a person approves a connection, so connections approved before this change show them after their next consent.

Saving a gateway's instructions as an unchanged copy of the built-in text now keeps the gateway on the built-in instructions. Before, the copy was stored as custom text, which dropped the line naming the servers. A gateway that already stores such a copy needs its instructions saved blank once. The built-in instructions for Direct gateways now say that each tool's name starts with its server.
