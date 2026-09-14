---
"server": minor
"dashboard": patch
---

Agent API keys can now poll `agent.getPlugins` when the agent holds the new `org:device_agent_sync` grant. The response includes the agent's principal, and plugins resolve for the agent, its roles, and the org wildcard. Adds the agent-runtime-safe `org:device_agent_sync` and `org:hooks_ingest` scopes in registry and delegated-policy version 2.
