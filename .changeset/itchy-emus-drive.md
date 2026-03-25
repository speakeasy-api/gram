---
"server": minor
---

Introduced faceted search capabilities to the audit logs, allowing users to filter logs based on actor and action attributes.

A new endpoint, `GET /rpc/auditlogs.listFacets`, is introduced to retrieve available facets for actors and actions. The existing `GET /rpc/auditlogs.list` endpoint is updated to support filtering by these facets.
