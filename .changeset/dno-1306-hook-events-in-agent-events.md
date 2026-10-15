---
"server": patch
---

Every hook event the hooks ingest endpoint records now also lands in agent_events: tool calls and their results, prompts and permission requests are classified with the tool, the session, the outcome and the duration filled, and the other hook events are kept under their own names, so what an agent's hooks report shows up next to the events the agent exports itself.
