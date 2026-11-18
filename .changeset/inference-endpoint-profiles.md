---
"server": minor
---

Add inference endpoint profiles: organization administrators define where Claude Code and Codex send inference (Amazon Bedrock, Google Vertex AI, or a gateway) and which people, roles, or directory groups use each profile. `agent.getPlugins` now returns the resolved settings in `tool_config` for the device agent to apply, and `inferenceProfiles.preview` shows what a person would receive and which rule decided it.
