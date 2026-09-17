# logs:read grant backfill

Observability reads now require the `logs:read` scope in addition to the
project or organization scope they already required. New organizations get it
from the system role defaults; organizations provisioned before the scope
existed do not, because `SeedSystemRoleGrants` skips a role that already holds
grants, and custom roles were never covered by it at all.

This offline application-data backfill closes that gap. It gives every
principal that already holds `org:read`, `org:admin`, `project:read`, or
`project:write` an unrestricted `logs:read` grant, so enforcing the new scope
changes nobody's access. Those four scopes are exactly the set the telemetry
handlers used to gate on, which makes the candidate set the set of principals
that could already read logs.

It writes only `principal_grants` rows and alters no schema. It is idempotent:
a principal that already holds `logs:read` — unrestricted or narrowed — is
never touched, so re-running after an administrator has narrowed a role does
not widen it back.

Run from `server/`. `GRAM_DATABASE_URL` must point directly at the intended
PostgreSQL database. Output contains aggregate counts only.

## Preview (default)

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations logs-read-grants \
  -environment=staging
```

The default mode reports the prospective grant count without writing.

## Apply

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations logs-read-grants \
  -apply \
  -environment=staging \
  -confirm-environment=staging \
  -confirm-target=db.example.internal:5432/gram \
  -confirm-apply=logs-read-grants
```

`-confirm-target` must exactly match the host, port, and database pgx parses
out of `GRAM_DATABASE_URL`, which ties write confirmation to the actual
database target rather than the descriptive environment label. Every apply also
requires `-confirm-apply=logs-read-grants`.

Each grant is inserted with `InsertPrincipalGrantIfAbsent`, so a partial run
can be resumed by re-running the same command. Stop on any error, investigate,
then re-run.

After applying, re-run the preview: it should report `principals=0
grants_added=0`. Role grant caches may retain pre-backfill values for up to
their normal TTL, so a dashboard session opened during the run can need a
refresh.
