---
id: admin
kind: exemption
summary: Staff-only tooling that is meant to see across tenants, behind admin authentication.
---

## When to use it

The query backs internal staff tooling whose entire purpose is the cross-tenant
view — the admin console listing organizations, inspecting a customer's projects
during a support request, or reconciling billing state. Scoping it to one tenant
would make it useless.

## Why it is safe

Crossing tenants is the intended behaviour, and the gate is the admin
authentication in front of it rather than a predicate in the SQL.

That places the entire boundary outside this query. Nothing in the statement
distinguishes legitimate staff tooling from an unscoped handler; only the route
it is mounted on does. This is the category most likely to be applied by
resemblance — a query that _looks_ administrative — so it is the one where naming
the actual gate matters most.

## Evidence required

Name the admin surface the query serves and the authentication that guards it.
Point at the concrete gate — the admin auth context, the route, the RBAC scope —
not at the query's name or its package.

`AdminListOrganizations` sitting in a package called `admin` is not evidence of
anything. The reviewable claim is that every route reaching this query requires
staff authentication, and that claim should be checkable in one hop from the
reason text.

## Example

```sql
-- name: AdminListOrganizations :many
-- sqlclint:ignore admin -- backs the staff admin console; every route reaching it
-- is mounted behind the admin session auther, which requires a Gram staff
-- identity rather than an organization membership
SELECT id, name, slug, created_at FROM organization_metadata
ORDER BY created_at DESC;
```

## When not to use it

Do not use it for a query merely because it lives in an admin-named package.
Packages get reorganised and queries get reused; the annotation must describe the
auth, not the folder.

Do not use it for an operation exposed to organization owners or any other
customer-facing elevated role. Those callers are still tenants, however
privileged, and their queries need a tenancy bound.

Do not use it for a support-impersonation path that assumes a specific tenant. If
the flow picks an organization, that organization is available to bind, and
binding it limits the blast radius of a mistake.
