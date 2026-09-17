# Okta connection ownership rollout

The `ownership_claimed` schema change must ship in a separate migration-only PR
and be applied before deploying application code that reads the column.

1. Keep the `okta-connections` create flag disabled during the migration and
   application rollout. Existing connections can still be read, verified, and
   revoked. This also prevents creates while Atlas replaces the unique index.
2. Apply the generated schema migration. Its `DEFAULT TRUE` conservatively
   preserves all existing reservations, including writes by old application
   instances. It must not default existing rows to unclaimed: doing so would
   silently release already verified tenants.
3. Deploy the updated application everywhere before enabling create. New creates
   explicitly insert `ownership_claimed = FALSE`, so pending connections no longer
   reserve public Okta URLs. Only successful token issuance establishes an
   exclusive claim; scope-error responses alone do not. Claims survive degraded
   verification and are released by revocation.
4. If connections predate this rollout, existing pending reservations are retained
   conservatively too. Have their owning organization revoke and recreate them
   through the API, or review them through the normal support process; do not
   bulk-clear claims or infer ownership solely from a pending/degraded status.
   This cleanup is unnecessary when the feature has never been enabled.

No production data changes or backfills are part of the migration.
