---
"server": minor
---

Record plugin actions in the audit log. Plugin create, update, delete,
server add/update/remove, role assignments, and publish each emit an
audit entry inside the same transaction as the mutation, surfacing the
events in `auditlogs.list` and the dashboard activity views.
