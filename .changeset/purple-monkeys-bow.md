---
"server": patch
---

Stop serializing the full role object into the after_snapshot column of the audit log when a role is created. This data bloats the database unnecessarily. A future dashboard update will link directly to the role instead for this audit log event.
