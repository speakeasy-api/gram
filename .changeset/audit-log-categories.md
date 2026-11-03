---
"server": minor
"dashboard": minor
---

Security activity such as an agent's runtime admitting its own workload identity is now recorded in its own log, separate from configuration changes. The audit log shows configuration changes by default and switches to security activity, where consecutive entries from the same agent are grouped by day. `auditlogs.list` and `auditlogs.listFacets` accept a `category` to choose the log, entries and webhook payloads carry it, and Kit runtime workload entries are attributed to the assistant's agent.
