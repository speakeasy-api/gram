---
id: stale-ignore-entry
kind: diagnostic
summary: An ignore-file entry names a query that is gone, renamed, or now compliant.
severity: error
silenced_by: []
---

## What it checks

An entry in the ignore file no longer corresponds to a live violation: the query
was deleted or renamed, or it now satisfies the tenancy rule on its own.

## Why it matters

The ignore file is a ratchet, not a suppression list. Its purpose is to record
exactly the debt that exists today so that the number can only go down, and so
that a reviewer can see the remaining surface at a glance by reading one file.

That only works if the file cannot drift upward in apparent size while the real
debt shrinks. Without this rule, entries accumulate for queries that were fixed
years ago, the file stops reflecting anything, and the ratchet becomes a
formality — present in CI, informative to nobody.

Firing on stale entries also makes fixing a query pleasant rather than annoying:
you scope a query correctly and the tool tells you there is a line to delete,
so the debt count visibly drops in the same commit that earned it.

## How to fix

Run `sqlclint run --write-ignore-file` to regenerate the file, then commit the
result. Removed lines in the diff are debt you actually paid off, and they belong
in the same commit as the fix that paid it.

If you renamed a query without changing its SQL, the regenerated file will show
one line removed and one added. That is expected; the entry is keyed by name.

Never hand-edit an entry to make an error go away. Adding or adjusting a line by
hand is how a real violation gets a permanent pass.

## Examples

### Violation

`.sqlclintignore` contains:

```
server/internal/toolsets/queries.sql::GetToolsetByID sha256:1c0ffee0b0de
```

while the query now reads:

```sql
-- name: GetToolsetByID :one
SELECT * FROM toolsets WHERE id = @id AND project_id = @project_id;
```

### Compliant

The entry is gone from `.sqlclintignore`, removed by `--write-ignore-file` in the
same commit that added the `project_id` predicate.

## Exemptions

None. This rule is about the ignore file's own consistency.
