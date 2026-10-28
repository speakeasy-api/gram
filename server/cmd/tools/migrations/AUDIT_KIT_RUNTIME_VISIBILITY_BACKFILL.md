# Kit runtime audit visibility backfill

This offline application-data backfill marks the Kit runtime's own workload admission entries as internal audit entries. Each Kit runtime admits its workload identity when it starts and withdraws it on teardown. Those entries record platform operations, not configuration changes, so they are hidden from the customer audit log and customer webhooks. Entries written after `audit_logs.visibility` existed are already marked; this run reclassifies the ones written before.

It changes only rows where `actor_type = 'system'`, `actor_id = 'kit-runtime'`, the action is `workload-admission:admit` or `workload-admission:withdraw`, and `visibility = 'customer'`. It does not alter the schema, delete rows, or publish webhook events.

Run from `server/`. `GRAM_DATABASE_URL` must point directly at the intended PostgreSQL database. The migration adding `audit_logs.visibility` must already be applied.

## Preview (default)

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations audit-kit-runtime-visibility \
  -environment=staging
```

Counts the rows an apply would change. Writes nothing.

## Apply

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations audit-kit-runtime-visibility \
  -apply \
  -environment=staging \
  -confirm-environment=staging \
  -confirm-target=db.example.internal:5432/gram \
  -confirm-apply=audit-kit-runtime-visibility
```

`-confirm-target` must exactly match the host, port, and database parsed from `GRAM_DATABASE_URL`. Rows are updated in batches of `-batch-size` (default 1000), each committing on its own, until none remain. The run is idempotent: an interrupted run can be restarted and continues with the rows still marked `customer`.

## Output

`mode=… environment=… rows=N`

In a dry run `rows` is the number of entries that would change; in an apply it is the number changed. On failure the command prints how many rows were changed before the error and exits non-zero.

## Recovery

To undo, set `visibility = 'customer'` on the same rows. Nothing else reads the column's previous value.
