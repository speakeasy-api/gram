---
"server": patch
---

Disabling an organization from the admin app or the Admin MCP now also locks its platform inference keys, and enabling it releases them. The lock is recorded as an `organization_disabled` cause, so any other cause already on a key is kept.
