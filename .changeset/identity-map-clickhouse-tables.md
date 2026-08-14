---
"server": patch
---

Add the ClickHouse identity_map fold tables (live + staging twin). Inert in
this release: the sync worker and analytics readers land separately. The map
folds each unambiguous directory or linked-account email to its owning user so
analytics queries can resolve one employee to one identity via joinGet.
