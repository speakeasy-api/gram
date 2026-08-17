---
"admin": patch
"server": patch
---

Opening a project from an organization record could hang forever. Every link
into a project now addresses it by id, and `project.get` resolves a slug to one
project before it reads that project's detail.

Project slugs are unique only within an organization, so a slug the whole
platform uses matches one project per organization. The detail read counts six
child tables for every row it matches, and two of those counts have no index on
`project_id` to use, so a common slug cost one full table scan per organization
and the read never returned. It now resolves the slug to a single id first and
counts once.
