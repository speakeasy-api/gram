---
"server": minor
"dashboard": minor
---

Assistant tool-call audit events no longer appear in the platform audit logs feed or its facets. They are surfaced instead on a new "Audit log" tab on the Assistants page, filterable by assistant, backed by new `subject_type` / `subject_id` filters on `auditlogs.list`.
