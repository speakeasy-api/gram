---
id: unknown-table-reference
kind: diagnostic
summary: Query references a name that is neither a schema table nor one of its own CTEs.
severity: error
silenced_by: []
---

## What it checks

Every table name the parser finds is resolved against the schema. Names bound by
the query itself — `WITH` clause CTEs — are subtracted first, since they are not
real tables and carry no tenancy of their own.

This rule fires on whatever is left: a name that exists in neither set.

## Why it matters

This is the guard on `sqlclint`'s own correctness rather than on your SQL.

The tenancy rule works by resolving each referenced table to its required
column. A name it cannot resolve contributes no requirement, so an unresolvable
table would make a query pass by default. That is the worst possible failure
direction for a security linter: the check reports success precisely because it
did not understand the query.

Rather than let that happen quietly, an unresolvable name is an error. In
practice it means the schema is stale relative to the queries, or a construct
binds a name in a way the resolver does not yet model.

## How to fix

Check the obvious causes in order. If the table was added in a recent migration,
regenerate `server/database/schema.sql` so `--schema-file` sees it. If the name
is misspelled, `sqlc generate` will reject it too.

If the name is bound by a construct `sqlclint` does not recognise as a
name-binding form, that is a gap in the resolver and the fix belongs in
`sqlclint/rule`, not in a suppression.

## Examples

### Violation

```sql
-- name: ListActiveToolsets :many
SELECT * FROM toolsts WHERE project_id = @project_id;
```

`toolsts` resolves to nothing, so the query would otherwise impose no tenancy
requirement at all and pass.

### Compliant

```sql
-- name: ListActiveToolsets :many
WITH active AS (
  SELECT * FROM toolsets WHERE project_id = @project_id AND deleted IS FALSE
)
SELECT * FROM active;
```

`active` is a CTE, so it is subtracted before resolution; `toolsets` resolves and
carries the requirement.

## Exemptions

None. An unresolvable name means the tool cannot reason about the query, and no
exemption category asserts that.
