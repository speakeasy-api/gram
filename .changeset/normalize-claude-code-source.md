---
"server": patch
---

Normalize the `Source` column on `chat_messages` for Claude Code hook
intake so tool-call messages use the OTEL `service.name` like user and
assistant messages, instead of hardcoding `ClaudeCode`.
