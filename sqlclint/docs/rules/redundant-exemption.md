---
id: redundant-exemption
kind: diagnostic
summary: A query is exempted but does not need it, or is both annotated and grandfathered.
severity: error
silenced_by: []
---

## What it checks

Two shapes:

- A query carries a `sqlclint:ignore` annotation but already passes the tenancy
  rule on its own.
- A query carries a `sqlclint:ignore` annotation and also has a grandfathered
  entry in the ignore file.

## Why it matters

An exemption is a standing claim that a query cannot be tenant-bounded. Once that
stops being true, the claim is misinformation sitting directly above the SQL —
and it is the kind a reader believes, because it is specific and it is adjacent
to the code.

The practical damage comes later. Someone extending a query reads
`parent-authorized` above it, takes the surrounding scoping as deliberate, and
preserves a shape that no longer needed preserving. Stale exemptions teach the
wrong pattern to the next person, and they inflate every count of how much
cross-tenant surface the system actually has.

The second shape — annotated and grandfathered at once — matters because the two
mechanisms mean different things. An annotation says "reviewed, and safe". An
ignore entry says "known debt, not yet reviewed". A query claiming both leaves
the reader unable to tell which is true, and lets a reviewed exemption keep a
debt entry alive long after the debt is gone.

## How to fix

If the query passes on its own, delete the annotation. It is not needed and its
presence is the whole problem.

If the query is annotated and also grandfathered, delete the ignore-file entry
and keep the annotation — the annotation is the reviewed, durable record.
`sqlclint run --write-ignore-file` does this for you.

## Examples

### Violation

```sql
-- name: ListProjectToolsets :many
-- sqlclint:ignore parent-authorized -- caller resolves the project first
SELECT * FROM toolsets WHERE project_id = @project_id AND deleted IS FALSE;
```

The query binds `project_id` directly, so nothing needed exempting.

### Compliant

```sql
-- name: ListProjectToolsets :many
SELECT * FROM toolsets WHERE project_id = @project_id AND deleted IS FALSE;
```

## Exemptions

None. This rule fires on an exemption, so exempting it would be circular.
