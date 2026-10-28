# Kit runtime audit category backfill

This offline application-data backfill moves audit entries about the Kit runtime's own workload admissions from the `configuration` category to `security`. Each Kit runtime admits its workload identity when it starts and withdraws it on teardown. Those entries record machine access, not configuration decisions, so they belong outside the audit log page, which shows configuration entries. They stay stored, visible to staff, and delivered as webhooks. Entries written after the application change that sets the category is deployed are already `security`; this run reclassifies the ones written before it, including any written between the `audit_logs.category` migration and that deploy.

It changes `workload-admission:admit` and `workload-admission:withdraw` rows still in `configuration` whose admission the Kit runtime admitted for itself, meaning the same organization has a `workload-admission:admit` row for that subject written by the `system:kit-runtime` actor. That includes withdrawals attributed to the person who withdrew the issuer. Tags are not used, because customers can set them. It does not alter the schema, delete rows, or publish webhook events.

Run from `server/`. `GRAM_DATABASE_URL` must point directly at the intended PostgreSQL database. The migration adding `audit_logs.category` must already be applied.

## Preview (default)

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations audit-kit-runtime-category \
  -environment=staging
```

Counts the rows an apply would change. Writes nothing.

## Apply

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations audit-kit-runtime-category \
  -apply \
  -environment=staging \
  -confirm-environment=staging \
  -confirm-target=db.example.internal:5432/gram \
  -confirm-apply=audit-kit-runtime-category
```

`-confirm-target` must exactly match the host, port, and database parsed from `GRAM_DATABASE_URL`. The URL must name a single host and require TLS (`sslmode=require`, `verify-ca` or `verify-full`). Rows are updated in batches of `-batch-size` (default 1000), each committing on its own, until none remain. No index covers `category`, so each batch scans `audit_logs`; raise `-batch-size` (up to 10000) when the preview count is large. The run is idempotent: an interrupted run can be restarted and continues with the rows still in `configuration`.

## Output

`mode=… environment=… rows=N`

In a dry run `rows` is the number of entries that would change; in an apply it is the number changed. On failure the command prints how many rows were changed before the error and exits non-zero.

## Recovery

The run records no list of changed rows, and entries written since the application started recording them as `security` match the same filter. To undo only this run, move matching rows created before that deploy back to `configuration`:

```sql
UPDATE audit_logs a
SET category = 'configuration'
WHERE a.subject_type = 'workload_admission'
  AND a.action IN ('workload-admission:admit', 'workload-admission:withdraw')
  AND a.category = 'security'
  AND a.created_at < '<deploy time of the application change, UTC>'
  AND EXISTS (
    SELECT 1 FROM audit_logs admit
    WHERE admit.organization_id = a.organization_id
      AND admit.subject_type = 'workload_admission'
      AND admit.subject_id = a.subject_id
      AND admit.action = 'workload-admission:admit'
      AND admit.actor_type = 'system'
      AND admit.actor_id = 'kit-runtime'
  );
```

Nothing else reads the column's previous value, and no webhook events are published either way.
