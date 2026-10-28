---
"server": minor
"dashboard": minor
---

Audit log entries now record a category: configuration changes, or security activity such as an agent's runtime admitting its own workload identity. The audit log shows configuration changes by default and switches to security activity, where consecutive entries from the same agent are grouped by day. `auditlogs.list` and `auditlogs.listFacets` accept a `category`, entries and webhook payloads carry it, and Kit runtime workload entries are attributed to the assistant's agent.
