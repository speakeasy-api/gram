---
"server": patch
---

Set a hard limit on concurrent HTTP requests to Gram Function runners deployed on Fly. This prevents OOM errors when a large number of tool calls are made in a short period of time. This can cause memory exhaustion and crashes.
