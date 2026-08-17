---
"admin": patch
---

Every link into a project now addresses it by id rather than by slug, so a
project named the same as one in another organization opens instead of hanging.

Project slugs are unique only within an organization. `project.get` resolves a
slug across all of them, so a common slug matches one project per organization
and the read never returns. The projects table, the row click and the record
nav's single-project shortcut all send the id.
