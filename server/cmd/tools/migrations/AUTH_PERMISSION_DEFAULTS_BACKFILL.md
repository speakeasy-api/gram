# Default auth permission backfill

This offline application-data backfill grants every organization the default auth permissions the mandatory auth client check (`mcp:auth`) needs, so enforcing it later removes no access people have today. It runs the grants phase of the auth client backfill (`authclientbackfill.RunGrants`) for each organization in turn; `docs/rbac.md` ("Default auth permissions") describes what it grants. It does not alter the schema.

Run from `server/`. `GRAM_DATABASE_URL` must point directly at the intended PostgreSQL database. Output contains aggregate counts only.

## Preview (default)

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations auth-permission-defaults \
  -environment=staging
```

Each organization's grants phase runs in a transaction that is always rolled back, taking no lock. The kind counts are the grants an apply would add. Only auth clients already recorded are counted, so run the upstream OAuth migration first where it applies.

## Apply

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations auth-permission-defaults \
  -apply \
  -environment=staging \
  -confirm-environment=staging \
  -confirm-target=db.example.internal:5432/gram \
  -confirm-apply=auth-permission-defaults
```

`-confirm-target` must exactly match the host, port, and database parsed from `GRAM_DATABASE_URL`. Each organization is applied in its own transaction under the auth client backfill's organization lock; an organization another run holds is skipped and counted as `busy`. The run is idempotent and only adds grants. Every grant it writes is recorded in `auth_permission_provisions`, and a grant recorded there is never written again, so a rerun does not restore one an admin removed.

## Output

`mode=… environment=… organizations=N personal_default=N shared_client=N api_key=N written=N keys_waiting=N busy=N failed=N`

- `keys_waiting`: organizations whose legacy API key policies were held back because some auth clients are not recorded yet. Run the upstream OAuth migration for them, then rerun.
- `failed`: organizations whose grants phase failed and rolled back. Rerun; a persistent failure needs investigation.

After applying, a preview should report zero kind counts apart from organizations still waiting or busy.

## Rollback

A rollback deletes the provisioned grants that are still live, then their ledger rows. It leaves:

- grants an admin took over by writing them (`taken_over_at` set), and
- every grant of an API key whose provisioned policy an admin took over in part. Removing the rest would leave the key a narrower policy than it had before the backfill.

Run it in a transaction, preview first, and scope it to one organization unless you mean all of them. After a rollback the ledger is gone, so a later run provisions again, including defaults admins had removed.

```sql
BEGIN;

-- Preview: grants that would be deleted.
SELECT count(*)
FROM principal_grants g
JOIN auth_permission_provisions p
  ON g.organization_id = p.organization_id
 AND g.principal_urn = p.principal_urn
 AND g.scope = p.scope
 AND g.selectors = p.selectors
WHERE p.taken_over_at IS NULL
  AND p.organization_id = '<ORG_ID>'
  AND NOT EXISTS (
    SELECT 1 FROM auth_permission_provisions t
    WHERE t.organization_id = p.organization_id
      AND t.principal_urn = p.principal_urn
      AND t.principal_urn LIKE 'api_key:%'
      AND t.taken_over_at IS NOT NULL
  );

DELETE FROM principal_grants g
USING auth_permission_provisions p
WHERE g.organization_id = p.organization_id
  AND g.principal_urn = p.principal_urn
  AND g.scope = p.scope
  AND g.selectors = p.selectors
  AND p.taken_over_at IS NULL
  AND p.organization_id = '<ORG_ID>'
  AND NOT EXISTS (
    SELECT 1 FROM auth_permission_provisions t
    WHERE t.organization_id = p.organization_id
      AND t.principal_urn = p.principal_urn
      AND t.principal_urn LIKE 'api_key:%'
      AND t.taken_over_at IS NOT NULL
  );

DELETE FROM auth_permission_provisions WHERE organization_id = '<ORG_ID>';

COMMIT;
```

For every organization, drop the two `organization_id = '<ORG_ID>'` conditions and the `WHERE` on the last statement, together. Never scope one statement without the others.

## Deploy and revert order

The migration adding `auth_permission_provisions` must be deployed before the server code that writes it: every explicit grant write now marks provisioned grants taken over. Revert that code before ever reverting the migration.

## Known gap

An admin adding a grant to a key's policy at the same moment the backfill tops it up can end up with both; a later run then leaves the policy alone.
