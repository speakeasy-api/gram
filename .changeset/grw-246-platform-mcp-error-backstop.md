---
"server": patch
---

Platform MCP tools no longer return raw database or internal error text to the caller. Any tool error that its own handling does not recognise is now logged with its full cause and answered with the generic `feature_unavailable` refusal, on both the external endpoint and the project assistant.
