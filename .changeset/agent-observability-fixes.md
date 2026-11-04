---
"server": patch
"dashboard": patch
---

Agents that run through hooks, such as OpenClaw, are observed correctly. Their calls show in the identity page's tool logs, their MCP calls are attributed to the server, prompts no longer count as tool calls, cron sessions are named after their job, and missing costs show as unknown instead of $0. Every failed gateway call is logged, gateways are shown by name, agent pages say whom the agent acts for on each server, and people who can edit an agent can read its sessions.
