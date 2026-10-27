---
"server": patch
"dashboard": patch
---

An agent's owner can now see the sessions their agent produced. Without `chat:read`, Agent Sessions lists your own sessions plus those of agents you own, and you can open them, but deleting one still needs `chat:write`. `chat.list` accepts an `agent_id` filter. The agent page's Overview and Sessions tabs list the agent's recent sessions and link to Agent Sessions filtered to that agent. The list of clients signed in as the agent is now labelled MCP connections.
