---
id: missing-tenant-scope
kind: diagnostic
summary: Query touches a tenant-scoped table without binding a tenancy parameter.
severity: error
silenced_by:
  [
    global-table,
    tenant-root,
    token-keyed,
    public-surface,
    background-sweep,
    admin,
    parent-authorized,
    cross-tenant-guard,
  ]
---

## What it checks

For every table a query references, `sqlclint` resolves which tenancy column that
table requires:

1. `project_id` is `NOT NULL` on the table, so it requires `project_id`.
2. Otherwise `organization_id` is `NOT NULL`, so it requires `organization_id`.
3. Otherwise the table requires at least one of the tenancy columns it has.

A table with neither `organization_id` nor `project_id` inherits the requirement
of whatever it reaches through its foreign keys. A table that reaches no
tenancy-bearing table at all is global and imposes no requirement.

The rule fires when a referenced table's required column is never bound to a
query parameter. The binding may appear anywhere in the statement — a `WHERE`
clause, a `JOIN ... ON` condition, an `EXISTS` or `IN (SELECT ...)` subquery, a
`HAVING` clause, the `WHERE` of an `UPDATE ... FROM`, a CTE, or an `INSERT`
column list. Position is not what matters; reaching the tenant boundary is.

## Why it matters

A query with no tenancy predicate is addressable by row id alone. Any caller who
learns or guesses an id — from a URL, a log line, an error message, an exported
document, a former colleague's screenshot — reads or writes another tenant's
data. This is insecure direct object reference, and it is the single most common
way multi-tenant systems leak across the boundary.

The risk is not evenly spread. An unscoped `SELECT` discloses; an unscoped
`UPDATE` or `DELETE` corrupts or destroys, and does so silently, because the
affected tenant has no way to observe a write they did not make.

## How to fix

Add the required tenancy column as a bound predicate and thread the value from
the authenticated context, never from the request payload.

For a table that carries the column directly, add it to the `WHERE` clause. For
a child table that has no tenancy column of its own, pin the tenant through the
parent row rather than trusting the child id:

```sql
WHERE h.remote_mcp_server_id = @remote_mcp_server_id
  AND EXISTS (
    SELECT 1 FROM remote_mcp_servers s
    WHERE s.id = h.remote_mcp_server_id AND s.project_id = @project_id
  )
```

If the query genuinely cannot be tenant-bounded, annotate it with the category
that explains why. Run `sqlclint rules --kind exemption` to see the vocabulary.

## Examples

### Violation

```sql
-- name: GetToolsetByID :one
SELECT * FROM toolsets WHERE id = @id AND deleted IS FALSE;
```

`toolsets.project_id` is `NOT NULL`, so any caller holding a toolset id reads it
regardless of which project owns it.

### Compliant

```sql
-- name: GetToolsetByID :one
SELECT * FROM toolsets
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE;
```

## Exemptions

Any category may silence this rule, but each one asserts a specific reason, and
the reason has to be true. `background-sweep` on a query reachable from an HTTP
handler is not an exemption, it is a mislabelled vulnerability. Prefer fixing the
predicate; reach for an annotation only when tenancy genuinely cannot be
expressed.
