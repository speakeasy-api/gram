---
"server": patch
---

Claude Code prompt correlation no longer stalls on high-volume sessions. Previously a chat with a large backlog of unlinked prompts could exceed the correlation time budget and fail entirely, leaving prompts unlinked from their telemetry; correlation now bounds its work and drains the backlog incrementally so prompts stay reliably linked.
