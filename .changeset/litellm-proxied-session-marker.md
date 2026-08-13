---
"server": minor
"dashboard": patch
---

Agent sessions routed through LiteLLM keep their LiteLLM association even when the agent's own hook stream captures the transcript: they match the LiteLLM platform filter and display as "<Client> via LiteLLM" in the session list and detail views.
