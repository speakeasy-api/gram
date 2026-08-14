---
id: nullable-tenant-param
kind: diagnostic
summary: The tenancy predicate is bound with sqlc.narg, so it can be disabled at runtime.
severity: error
silenced_by: [admin, background-sweep]
---

## What it checks

The tenancy predicate is bound with `sqlc.narg(...)` rather than `@name`,
`sqlc.arg(...)`, or `$n`. `sqlc.narg` generates a nullable Go parameter, so the
value reaching Postgres can be `NULL`.

Only non-nullable binds count as satisfying a tenancy requirement.

## Why it matters

A nullable tenancy parameter has two failure modes and neither is acceptable for
a security boundary.

Written plainly as `project_id = sqlc.narg('project_id')`, a `NULL` makes the
comparison `NULL`, the row never matches, and the query silently returns nothing
— a scoping check that fails closed but produces empty reads and no-op deletes
that are hard to trace.

Written in the common filter idiom, it fails open:

```sql
WHERE (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id'))
```

Passing `NULL` removes the predicate entirely and the query returns every
tenant's rows. If that parameter is populated from a request field, an attacker
disables your tenancy boundary by omitting a key from a JSON body.

The distinction between the two forms is invisible at the call site — both are
just a `*uuid.UUID` in Go — which is why this rule rejects the nullable bind
outright rather than trying to tell them apart.

## How to fix

Make the tenancy bind non-nullable and keep the optional filter separate. A list
endpoint that lets a user narrow to one project still has a mandatory
organization boundary around it:

```sql
WHERE organization_id = @organization_id
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id'))
```

The outer `@organization_id` is the security boundary and is always applied. The
inner nullable predicate is a user-facing filter and is allowed to be absent,
because widening it can only ever reach rows the outer bound already permits.

## Examples

### Violation

```sql
-- name: ListAuditLogs :many
SELECT * FROM audit_logs
WHERE (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id'))
ORDER BY created_at DESC;
```

### Compliant

```sql
-- name: ListAuditLogs :many
SELECT * FROM audit_logs
WHERE organization_id = @organization_id
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id'))
ORDER BY created_at DESC;
```

## Exemptions

`admin` and `background-sweep` cover callers already operating across tenants,
where a nullable filter carries no boundary. No other category applies: if a
query needs a tenancy bound at all, that bound must not be nullable.
