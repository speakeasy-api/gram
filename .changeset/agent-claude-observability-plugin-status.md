---
"server": minor
---

Device agents can ask whether to offer the Claude observability plugin and whether the copy a user uploaded to claude.ai is sending events. `GET /rpc/agent.getClaudeObservabilityPluginStatus` reports `available` (false when the organization has turned observability off) and when the user's plugin key was minted and last used. Like the download, it needs the user's own key.
