---
id: cross-tenant-guard
kind: exemption
summary: The query must ignore tenancy to remain a correct fail-safe.
---

## When to use it

The query exists to answer a question about rows the caller deliberately cannot
see, and scoping it would make its answer wrong in the unsafe direction. The
recurring case is a count that guards a destructive operation: "is anything still
attached to this issuer?" must consider every attachment, not only the ones this
tenant can observe.

## Why it is safe

The query returns an aggregate — a count, an existence check — rather than rows.
Nothing tenant-specific crosses the boundary, and the value is consumed by a
guard rather than shown to a user.

A scoped version would be actively dangerous here. A count that skipped invisible
rows would report zero while live clients remained, and the delete it guards
would proceed and strand them. Tenancy scoping is normally the safe default;
this is the narrow case where it is the failure.

## Evidence required

State what the value guards and why a scoped count would produce the wrong
answer. Then confirm that only the aggregate leaves the query — no ids, no names,
nothing attributable to another tenant.

If the result is ever surfaced to a user rather than consumed by a guard, say so
and explain why the number itself discloses nothing. A count of another tenant's
rows is still a small leak, and sometimes an informative one.

## Example

```sql
-- name: CountRemoteSessionClientsByIssuerID :one
-- sqlclint:ignore cross-tenant-guard -- counts every non-deleted client on the
-- issuer across all tenancy tiers; the delete guard relies on this, since a count
-- that skipped rows the caller cannot see would let the delete strand live
-- clients. Only the count is returned.
SELECT count(*)::bigint FROM remote_session_clients
WHERE user_session_issuer_id = @issuer_id AND deleted IS FALSE;
```

## When not to use it

Do not use it for a query that returns rows. If ids or names come back, this is a
cross-tenant read wearing a guard's clothing.

Do not use it for a count displayed in a tenant-facing UI without saying why the
number is safe to show. "N servers are attached" can disclose more than it looks.

Do not use it as a general escape hatch for aggregates. An ordinary `COUNT` on a
dashboard is a normal scoped query and should bind its tenant like any other.
