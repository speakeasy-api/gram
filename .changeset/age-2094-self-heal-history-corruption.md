---
"server": minor
---

assistants now self-heal when the inference provider rejects a chat as malformed: the runtime trims history to the last 5 user messages, prepends a recovery notice that nudges the agent to recover lost context via its tools, and retries — instead of leaving the thread stuck.
