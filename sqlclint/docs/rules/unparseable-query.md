---
id: unparseable-query
kind: diagnostic
summary: libpg_query could not parse the query, so its tenancy cannot be verified.
severity: error
silenced_by: []
---

## What it checks

`sqlclint` parses every query with libpg_query, the same Postgres grammar sqlc
itself uses. This rule fires when that parse fails.

## Why it matters

The rule exists to make `sqlclint` fail closed. A linter that skips what it
cannot understand reports a clean run over a corpus it only partly inspected,
and the queries it silently skipped are exactly the unusual ones most worth
looking at.

Because the parser is the same one sqlc uses, a query that fails here would also
fail `sqlc generate`. In practice this rule fires on a syntax error you were
about to hit anyway, or on a Postgres extension whose grammar libpg_query does
not yet cover.

## How to fix

Read the parser message; it carries a byte offset into the query. If it is a
plain syntax error, fix it — `sqlc generate` would have rejected the same text.

If the query is valid Postgres that libpg_query cannot handle, rewrite the
offending fragment into a form it accepts. Moving an exotic operator behind a
plain function call is usually enough, and it keeps the query verifiable rather
than exempt.

This rule cannot be silenced by an annotation. An unverifiable query is not an
approved cross-tenant query; it is a query nobody has checked.

## Examples

### Violation

```sql
-- name: GetToolset :one
SELECT * FROM toolsets WHERE id = @id AND;
```

The trailing `AND` leaves the parser mid-expression at end of input.

### Compliant

```sql
-- name: GetToolset :one
SELECT * FROM toolsets WHERE id = @id AND project_id = @project_id;
```

## Exemptions

None. `silenced_by` is deliberately empty: there is no category whose meaning is
"we could not check this one".
