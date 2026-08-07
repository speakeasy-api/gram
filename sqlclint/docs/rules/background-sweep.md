---
id: background-sweep
kind: exemption
summary: A worker job that intentionally spans every tenant, with no request context.
---

## When to use it

The query runs from a scheduled job, Temporal activity, or reaper that operates
over the whole table by design — outbox garbage collection, expiring stale
sessions, advancing poll cursors, reaping soft-deleted rows past their retention
window. Scoping it to one tenant would defeat its purpose.

## Why it is safe

There is no caller and no request, so there is no attacker-supplied input to
abuse. The job selects rows by age, state, or schedule, and those predicates are
not reachable from any API surface.

Everything about that safety comes from the _call graph_, not from the SQL. The
identical query invoked from an HTTP handler is an unscoped cross-tenant write.
This category is therefore a claim about who calls the query, and it stays true
only while that stays true.

## Evidence required

Name the job or activity that runs it, and state that no request-scoped caller
reaches it. Then name the predicate that bounds the sweep — the age cutoff, the
status filter, the schedule due-time.

That second half matters more than it looks. "Runs in the background" permits a
`DELETE` with no `WHERE` at all. The bound is what keeps a sweep a sweep, and it
is what a reviewer checks when the query later grows a new clause.

## Example

```sql
-- name: GCProcessedOutboxRows :execrows
-- sqlclint:ignore background-sweep -- outbox GC activity in
-- internal/background/activities; runs on a timer with no request context and
-- deletes only rows already processed before the retention cutoff
DELETE FROM outbox
WHERE processed_at IS NOT NULL AND processed_at < @cutoff;
```

## When not to use it

Do not use it for a query that a handler also calls. Shared query, shared risk —
if any path reaches it with a request context, it needs a tenancy bound and the
background caller can pass one too.

Do not use it for an admin-triggered operation, even one that runs
asynchronously. A human starting a job with parameters is a caller; that is
`admin`, which carries a different claim.

Do not use it for a query that takes an id, a name, or any other value from
outside the job itself. The moment a sweep accepts caller input, it is not a
sweep.
