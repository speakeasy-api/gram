# Pro organization entitlement backfill

This offline application-data backfill gives every organization whose `gram_account_type` is `pro` the shared enterprise-access entitlement bundle. It calls the same `productfeatures.SeedEnterpriseAccessEntitlementsTx` seeder used for PAYG activation; it does not alter the schema or Atlas migrations. Existing soft-deleted feature rows are treated as explicit disables and are never restored.

Run from `server/`. `GRAM_DATABASE_URL` must point directly at the intended PostgreSQL database. Output contains aggregate counts only.

## Preview (default)

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations pro-entitlements \
  -environment=staging
```

The default mode runs the shared entitlement seeder for every candidate in a transaction that is always rolled back. `features_added` is therefore the prospective insert count, including the same soft-delete preservation behavior as apply mode. No writes are committed.

## Apply

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations pro-entitlements \
  -apply \
  -environment=staging \
  -confirm-environment=staging \
  -confirm-target=db.example.internal:5432/gram \
  -confirm-apply=pro-entitlements
```

`-confirm-target` must exactly match the host, port, and database parsed from `GRAM_DATABASE_URL` by pgx; this ties write confirmation to the actual database target rather than the descriptive environment label. Every apply also requires `-confirm-apply=pro-entitlements`.

Each candidate is handled in its own transaction. After taking a row lock, the command rechecks `gram_account_type` and skips the organization if it is no longer `pro`. The operation is idempotent: rerunning it inserts only never-configured entitlements. Stop on any error, investigate, then rerun the same command.

After applying, rerun the preview and inspect `organizations` plus `features_added`. A subsequent preview or apply should report `features_added=0`. Application feature caches may retain pre-backfill values for up to their normal TTL.
