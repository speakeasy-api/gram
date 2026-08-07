---
id: wrong-tenant-column
kind: diagnostic
summary: Query binds a tenancy column, but not the one the table requires.
severity: error
silenced_by: [admin, background-sweep, cross-tenant-guard, parent-authorized]
---

## What it checks

The query binds a tenancy parameter, but a different one from what the table
requires. Two shapes trigger it:

- The table has a non-nullable `project_id`, and the query binds only
  `organization_id`.
- The table has a non-nullable `organization_id` and a nullable `project_id`,
  and the query binds only `project_id`.

The required column is always the narrowest one guaranteed to be present. A
nullable column cannot be that, because rows where it is `NULL` fall out of any
equality predicate written against it.

## Why it matters

Binding the wrong column looks scoped and reads as scoped in review, which is
what makes it worse than binding nothing at all.

Scoping a project-scoped table by `organization_id` bounds the query to the
organization but lets it cross every project inside it. On a large tenant that is
dozens of projects that were never meant to see each other. This is the exact
shape of an org-level cascade that quietly widened.

Scoping by a nullable `project_id` where `organization_id` is the guaranteed
column is worse still: rows with a `NULL` project silently vanish from the result
set. Reads return incomplete data and deletes leave rows behind, so the failure
surfaces later as a correctness bug whose cause is long gone.

## How to fix

Bind the column the table requires. Keep the other one if it usefully narrows the
query further — an extra `organization_id` predicate alongside a required
`project_id` is redundant but harmless, and this rule never asks you to remove
it.

When the query really does need to span every project under an organization,
that is a deliberate cascade. Annotate it and state in the reason which caller
verifies the parent belongs to the organization first.

## Examples

### Violation

```sql
-- name: ListOrganizationToolsets :many
SELECT t.* FROM toolsets t
JOIN projects p ON p.id = t.project_id
WHERE p.organization_id = @organization_id;
```

`toolsets.project_id` is `NOT NULL`, so this returns every project's toolsets.

### Compliant

```sql
-- name: ListProjectToolsets :many
SELECT * FROM toolsets WHERE project_id = @project_id AND deleted IS FALSE;
```

## Exemptions

`admin` and `background-sweep` cover callers that legitimately operate above the
project boundary. `cross-tenant-guard` covers counts that must span tiers to stay
a correct fail-safe. `parent-authorized` covers a cascade whose parent row the
caller has already authorized — the reason text must name that caller.
