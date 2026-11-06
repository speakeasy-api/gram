---
"server": minor
"dashboard": minor
---

The audit log page now opens on a **Changes** view that hides Speakeasy's own background activity, such as publishing plugins and creating the API keys they use. That activity moved to a new **System** view, **All changes** shows everything, and **Security activity** is unchanged. Older `?category=configuration` links open the Changes view. The `auditlogs.list` and `auditlogs.listFacets` endpoints take a new optional `origin` filter (`user` or `system`).
