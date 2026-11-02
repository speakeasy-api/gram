---
"dashboard": patch
"server": patch
---

The agent wizard now checks up front whether you may give the agent `mcp:auth`. Where the organization enforces upstream sign-in, a refusal stops the wizard on its first step and says who lacks the permission, instead of failing when the agent is created. Elsewhere the agent is created without the grant, since sign-in does not need it yet. Product features now report whether upstream auth enforcement is on.
