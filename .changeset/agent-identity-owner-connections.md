---
"dashboard": minor
"server": patch
---

The New agent identity flow can now pick servers that sign in as a person. The agent uses its creator's own connection to them, set up when the agent is created, and each server shows whose connection it uses and when that connection ends. Only an agent's owner can let it use a connection: `remoteSessions.attachBinding` now refuses anyone else, since the agent would never use their connection.

Servers appear in one list ordered by tool activity over the last day, with shared access labeled separately. Assigned connections show the owner's avatar, remain read-only for other members, and can be refreshed inline. Expired assignments are flagged in the agents list and overview. Agent gateways now resolve each server's assigned owner connection when executing tools.
