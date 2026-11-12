---
"server": minor
---

Organization admins can target an MCP tool-call kill switch at a registered agent instead of a user, through `user_id` or `agent_id` on the killswitches API. A new `killswitches.batchAgentBadges` endpoint reports which agents a switch affects now or on a schedule, and `agents.list` now returns `last_credential_used_at` for callers who can manage an agent's credentials.
