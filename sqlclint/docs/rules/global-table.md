---
id: global-table
kind: exemption
summary: Every table the query touches is a platform catalog with no tenant column.
---

## When to use it

The query reads or writes only tables that belong to the platform rather than to
any tenant: `global_roles`, `mcp_registries`, `integrations`, and similar
catalogs. There is no `organization_id` or `project_id` anywhere in reach, and no
foreign key that leads to one.

`sqlclint` already recognises the clean case without an annotation — a query
touching only tables it classifies as global passes on its own. Use this category
when a query mixes a global table with one the tool considers tenant-reachable,
and the tenant-reachable side is genuinely catalog data too.

## Why it is safe

The rows are the same for every tenant. There is no boundary to cross because
there is nothing tenant-specific to reach: two organizations reading
`global_roles` are entitled to identical results.

Note what this does not cover. "Nobody can currently reach it" is not the same as
"the rows are not tenant data". A table holding one tenant's rows is not global
merely because the current caller happens to be internal.

## Evidence required

Name the tables and state that they hold platform data rather than tenant data.
If the schema makes this obvious, one line is enough.

If the query also touches a table with a tenancy column, say why that table's
rows are catalog data in this context — this is the case the annotation actually
exists for, and it is the one a reviewer needs.

## Example

```sql
-- name: ListGlobalRoles :many
-- sqlclint:ignore global-table -- global_roles is the platform role catalog; the
-- same rows are visible to every organization and it has no tenancy column
SELECT * FROM global_roles WHERE deleted IS FALSE ORDER BY slug;
```

## When not to use it

Do not use it because a table happens to lack a `project_id` column. Most such
tables are child rows whose tenant is reachable through a foreign key — headers
belonging to a server, versions belonging to a skill. Those need
`parent-authorized` and an actual join, not a claim of globality.

Do not use it for tables that are global today only because the feature is young.
If tenant scoping is a plausible future for the table, the query needs a real
bound before that day, not an annotation asserting it never will.
