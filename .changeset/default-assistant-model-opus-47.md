---
"server": patch
"dashboard": patch
---

Upgrade the default assistant model to Claude Opus 4.7. The platform-managed Project Assistant, the assistant onboarding flow, and the onboarding system prompt's default recommendation now use `anthropic/claude-opus-4.7` instead of `anthropic/claude-sonnet-4.6`. Existing assistants are unaffected; only newly created assistants pick up the new default.
