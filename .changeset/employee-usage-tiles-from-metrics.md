---
"dashboard": patch
---

Read the employee page's Total Tokens and Total Cost tiles from the per-user
metrics query instead of the employees-list summary. The list query groups a
person's telemetry by identity, so those tiles were showing one identity's slice
of the usage — for someone on a personal AI account, often the slice with no
tokens or cost in it. The metrics query aggregates the same person's rows
without grouping them, so the tiles now show their whole total.
