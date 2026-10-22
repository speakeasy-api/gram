# Remote session client scope backfill

Before #7199, an automatically created remote session client stored a copy of the scopes discovered at creation: the protected resource's `scopes_supported`, or the issuer's whole `scopes_supported` when the resource advertised none. A stored client scope wins over challenge scopes, the resource pin and the provider override, so those copies go stale and, for the issuer fallback, request everything the provider can grant (AIM-288). This backfill sets `remote_session_clients.scope` to NULL on clients whose scope is provably such a copy, so their logins discover scopes live. AIM-541.

A copy cannot be told apart from an operator's choice directly, so a client is cleared only when all of these hold:

- live, created before `-created-before`, with a non-empty scope
- not managed by an identity provider connection, owned by its subject (`credential_owner = 'subject'`), not on the legacy callback, and not platform-global
- no `remote-session-client:update` audit entry changed `Scope` (an update entry missing either snapshot counts as a change)
- its scope equals, as a set, the `scopes_supported` of its issuer or of the protected resource of a remote MCP server it is bound to

Anything else is left alone and reported as a skip. A strict subset of the issuer's list is not cleared: it may be an old copy taken when the catalogue was smaller, or an operator's pick.

Run from `server/`. `GRAM_DATABASE_URL` must point at the primary. Stdout carries aggregate counts only; ids and scopes go to the `-report` file, which stays on the operator's machine.

## Preview (default)

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations client-scopes \
  -environment=prod \
  -created-before=<#7199 deploy time, RFC 3339> \
  -report=/tmp/client-scopes-preview.json
```

Use the time #7199 reached the environment, not its merge time: clients created between the two still received a copy.

## Apply

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations client-scopes \
  -apply \
  -environment=prod \
  -confirm-environment=prod \
  -created-before=<same value as the preview> \
  -report=/tmp/client-scopes-apply.json
```

`-apply` requires a report path and refuses an existing file. Each client is cleared in its own transaction: the row is locked, the evidence is rechecked, the update compares `updated_at`, and a `remote-session-client:update` audit entry with actor `system:client-scopes-backfill` records the before and after snapshots. A client that changed between the scan and the lock is reported as `skip_changed`. The run is idempotent; a rerun finds nothing to clear.

## Restore

The apply report holds each cleared client's previous scope. To put them back:

```sh
psql "$GRAM_DATABASE_URL" -v report="$(cat /tmp/client-scopes-apply.json)" <<'SQL'
UPDATE remote_session_clients AS c
SET scope = ARRAY(SELECT jsonb_array_elements_text(r->'scope')), updated_at = clock_timestamp()
FROM jsonb_array_elements(:'report'::jsonb->'rows') AS r
WHERE r->>'outcome' = 'cleared'
  AND c.id = (r->>'remote_session_client_id')::uuid
  AND c.scope IS NULL
  AND c.deleted IS FALSE;
SQL
```
