---
"server": minor
"dashboard": minor
---

Agent Sessions now shows who an assistant session ran as and on whose behalf. The assistant name links to the agent identity it acts as, or to the assistant when it has no agent identity, and the member it acted for is shown and linked next to it, in both the session list and the session header. Session owners in the list are now clickable. Chat listings and `loadChat` return `assistant_agent_id` for assistant sessions backed by an agent identity.
