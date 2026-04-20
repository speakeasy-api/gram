---
"server": patch
---

Move plugins from project-scoped to org-scoped. Drops project_id from
the plugins table and updates the unique slug index to (organization_id, slug).
