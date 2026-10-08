---
"server": patch
---

Keep the Anthropic Compliance import running when one Claude chat's content is no longer retrievable. Anthropic answers 404 for chats hard-deleted through the Compliance API or by retention while the activity feed and the chat list can still name them. The importer now skips such a chat and reports it as unavailable instead of failing the whole poll, which previously stalled the integration on that chat and counted toward auto-pause.
