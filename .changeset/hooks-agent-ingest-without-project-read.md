---
"server": patch
---

Let agent API keys send hook events to a project without holding `project:read` there. On the hook ingest endpoints, an agent key is admitted to a project by its `org:hooks_ingest` grant for that project alone. Other callers and every other endpoint still require `project:read`.
