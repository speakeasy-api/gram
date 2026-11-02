---
"dashboard": patch
---

The agent wizard now checks whether you may give the agent `mcp:auth` as soon as you choose a server, on create and on edit. Where the organization enforces upstream sign-in, a refusal stops the wizard on the server step and says who lacks the permission, instead of failing when the agent is saved. Elsewhere the agent is created without the grant, since sign-in does not need it yet.
