---
"server": patch
---

Cascades okta_resource_connections from the snapshot app instance instead of a partial SET NULL that nulled the tenant columns and made revoke fail.
