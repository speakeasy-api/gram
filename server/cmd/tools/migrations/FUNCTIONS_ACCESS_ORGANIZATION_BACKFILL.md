# Function runner key organization backfill

This offline application-data backfill sets `functions_access.organization_id` on function runner keys written before the deploy path set it. Sealing runner keys with an organization's own key needs the organization on the row: the organization encryption fence compares against it, and the key copies are sealed for it.

A runner key's organization is its project's organization. A runner key keeps a NULL `organization_id`, and stays on the platform key, when its project's organization has no `organization_metadata` row. `projects.organization_id` has no foreign key, but `functions_access.organization_id` does.

The run covers soft-deleted runner keys. It changes only `organization_id`. It never touches `updated_at` or the key itself, so every deployed function runner keeps working. It never overwrites an `organization_id` that is already set. It does not alter the schema or publish events.

Run from `server/`. `GRAM_DATABASE_URL` must point directly at the intended PostgreSQL database.

## Before running

1. The application change that makes the deploy path set `organization_id` is deployed. From then on, new runner keys get the column.
2. No image built before that change is still a rollback target. An older image inserts runner keys without `organization_id`. If one was rolled back to, run the backfill again: it is idempotent.

## Preview (default)

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations functions-access-organization \
  -environment=staging
```

Counts runner keys by category. Writes nothing. A preview still sends credentials, so it requires TLS too unless every host in `GRAM_DATABASE_URL` is on this machine (loopback, `localhost`, or a Unix socket).

## Apply

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations functions-access-organization \
  -apply \
  -environment=staging \
  -confirm-environment=staging \
  -confirm-target=db.example.internal:5432/gram \
  -confirm-apply=functions-access-organization
```

`-confirm-target` must exactly match the host, port, and database parsed from `GRAM_DATABASE_URL`. The URL must name a single host and require TLS (`sslmode=require`, `verify-ca` or `verify-full`).

The run walks runner keys without an `organization_id` in id order, `-batch-size` rows at a time (default 500, at most 10000). Each batch is one statement that commits on its own. Every function tool call reads this table, so keep batches small. An interrupted run continues when run again.

After applying, run the preview again and confirm `pending=0`. If it is not 0, an older image wrote runner keys during the run, or an organization was deleted mid-run (which fails the run with a foreign key error). Apply again.

## Output

`mode=… environment=… filled=N pending=N missing_organization=N`

- `filled`: runner keys this run updated. Always 0 in a preview.
- `pending`: runner keys still waiting to be filled, counted after the run. In a preview, that is what an apply would fill. After an apply, it is 0 unless something listed above left rows behind.
- `missing_organization`: runner keys whose project is gone or whose project's organization has no metadata row. They stay NULL and on the platform key. Investigate any nonzero count before finalizing an organization on its own key.

Counts never name organizations. Do not record organization IDs in tickets or pull requests.

On failure the command prints how many runner keys were filled before the error and exits non-zero.

## Recovery

The deploy path sets the same value this backfill does, so there is normally nothing to undo. Nothing reads `organization_id` until sealing with organization keys ships, so clearing it is safe before then. Clearing cannot tell backfilled rows from rows the deploy path set, so it clears both; run the backfill again to restore them. Clear in batches, repeating until it updates 0 rows, so no statement locks much of the table:

```sql
UPDATE functions_access SET organization_id = NULL
WHERE id IN (
  SELECT id FROM functions_access WHERE organization_id IS NOT NULL LIMIT 500
);
```

Once sealing reads the column, do not clear it: runner keys sealed with an organization's key need it to be opened.
