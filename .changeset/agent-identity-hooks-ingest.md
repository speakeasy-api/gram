---
"dashboard": patch
"server": patch
---

A new agent identity can now send hook events from the observability plugin. When its creator holds `org:hooks_ingest`, the agent is created with that grant limited to its project, and its first key requests it. The agent install script no longer says `project:read` is needed to send hooks.
