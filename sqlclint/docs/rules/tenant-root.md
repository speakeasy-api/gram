---
id: tenant-root
kind: exemption
summary: The row's own primary key is the tenant identifier.
---

## When to use it

The query addresses an organization or a project by its own id or slug —
`organization_metadata`, `projects`, and the tables keyed directly by them. There
is no separate tenancy column to bind because the key already is the tenant.

## Why it is safe

`WHERE id = @organization_id` is a tenancy predicate. Adding
`AND organization_id = @organization_id` to a row whose primary key is that same
value would be a tautology, not defence in depth.

The safety here rests entirely on where the id comes from, not on the SQL. The
query is exactly as safe as its caller's authorization check and no safer, which
makes this the category whose reason text carries the most weight.

## Evidence required

State that the key being bound is the tenant identifier, and name what
establishes the caller's right to it — the authorization check, the middleware,
or the RBAC scope that runs before this query.

"Keyed by org id" alone is not evidence; every IDOR is keyed by something. The
reviewable claim is that the caller was proven to hold that org before the query
ran.

## Example

```sql
-- name: GetOrganizationMetadata :one
-- sqlclint:ignore tenant-root -- organization_metadata is keyed by the organization
-- id itself; callers pass ActiveOrganizationID from the authenticated context,
-- which the auth middleware establishes before any handler runs
SELECT * FROM organization_metadata WHERE id = @id;
```

## When not to use it

Do not use it for a table that merely has an `organization_id` column. That is an
ordinary tenant-scoped table and the column should be bound normally; this
category is only for the row that _is_ the tenant.

Do not use it when the id arrives from a request payload rather than the
authenticated context. That is the textbook IDOR this whole check exists to
catch, and labelling it `tenant-root` documents the vulnerability instead of
fixing it.
