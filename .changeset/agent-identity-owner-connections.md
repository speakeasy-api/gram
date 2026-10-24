---
"dashboard": minor
"server": patch
---

The New agent identity flow can now pick servers that sign in as a person. The agent uses its creator's own connection to them, set up when the agent is created, and each server shows whose connection it uses and when that connection ends. Only an agent's owner can let it use a connection: `remoteSessions.attachBinding` now refuses anyone else, since the agent would never use their connection.
