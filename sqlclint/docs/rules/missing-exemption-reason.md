---
id: missing-exemption-reason
kind: diagnostic
summary: A sqlclint:ignore annotation carries a category but no reason text.
severity: error
silenced_by: []
---

## What it checks

A `sqlclint:ignore` annotation is missing the `-- <reason>` half, or the reason
is only whitespace. The full form is:

```
-- sqlclint:ignore <category> -- <reason>
```

## Why it matters

The category says which kind of exemption this is. The reason says why it is
true of this query, and that is the part a reviewer can actually check.

`background-sweep` on its own is a claim. "Runs only from the outbox GC activity,
which has no request context" is a claim someone can verify, and can notice has
become false when that query later acquires an HTTP caller. Without the reason,
an exemption ages into an unfalsifiable assertion that nobody can re-examine,
because nothing records what it was asserting.

The reason is also the only thing that survives into an audit. When someone asks
which queries deliberately cross tenants and why, the categories give the shape
and the reasons give the answer.

## How to fix

State what makes this query safe, in terms someone could check against the code.
Name the caller, the credential, or the invariant.

The useful test: if this exemption were wrong a year from now, would the reason
text be visibly false? "Internal query" would not be. "Called only from
`internal/background/activities/outbox.go`, which runs without an auth context"
would be.

Each exemption document has an `## Evidence required` section stating what its
reason is expected to establish.

## Examples

### Violation

```sql
-- name: GCProcessedOutboxRows :execrows
-- sqlclint:ignore background-sweep
DELETE FROM outbox WHERE processed_at < @cutoff;
```

### Compliant

```sql
-- name: GCProcessedOutboxRows :execrows
-- sqlclint:ignore background-sweep -- outbox GC activity sweeps every tenant's
-- processed rows by age; it runs on a timer with no request context and no caller-
-- supplied input
DELETE FROM outbox WHERE processed_at < @cutoff;
```

## Exemptions

None. An annotation that does not explain itself is not an exemption.
