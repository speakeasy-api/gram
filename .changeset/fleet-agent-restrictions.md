---
"server": minor
"dashboard": minor
---

Add a project Fleet list and department directory with explicit registered-agent, assistant, and captured-session attribution, limited to observed activity in the last 24 hours. Reuse identity, transcript, Security, and kill-switch experiences with responsive inspectors.

Extend the curated customer kill-switch API to agent principals, including overlap previews, historical reads, and batchAgentBadges. Summary and detail now expose principal_kind and agent_id; user_id is optional and remains populated only for user targets. Existing callers default to user restrictions. Agent restrictions block covered MCP tools/call across credential sessions; Release removes only the selected restriction without restarting execution or changing suspension.

Expose optional lastCredentialUsedAt on agents.list for callers authorized to manage an agent’s credentials. One batch combines recorded credential-session and agent API-key authentication history across the organization, including credentials since revoked or expired. Fleet applies the rolling window before captured-session pagination and derives assistant activity only from explicitly linked sessions loaded during the current organization/user/project visit, preserving recent evidence across searches and pages. Agent restriction recovery remains available regardless of activity age.
