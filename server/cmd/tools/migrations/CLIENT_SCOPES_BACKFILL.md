# Remote session client scope backfill

Before #7199, an automatically created remote session client stored a copy of the scopes discovered at creation: the protected resource's `scopes_supported`, or the issuer's whole `scopes_supported` when the resource advertised none. A stored client scope wins over challenge scopes, the resource pin and the provider override, so the copy goes stale and hides those settings. This backfill sets `remote_session_clients.scope` to NULL where the scope is a copy, so logins resolve it the way they do for clients created since. AIM-541.

A client is cleared only when all of these hold:

- live, with a live issuer that belongs to its project, its organization, or is platform-global, and a live project when project-scoped, created before `-created-before`, with a non-empty scope
- linked to a user session issuer (every automatic create path linked one)
- not managed by an identity provider connection, not a trusted sign-in client of a user session issuer, not bound for identity chaining (an unlinked binding does not count), owned by its subject, not on the legacy callback, and not platform-global
- no `remote-session-client:update` audit entry changed `Scope` (an entry missing either snapshot counts as a change)
- its scope equals, as a set, the issuer's `scopes_supported` or the last-read `scopes_supported` of a bound server's protected resource
- for every server signing in through it, a login with no client scope would request that same set (`skip_request_changes` otherwise)

The last rule makes the clear change nothing a login sends today, so providers that hold a client to its registered scopes keep accepting it. Clients it skips because a pin, override or challenge would now apply are left for an operator to decide.

Run from `server/` after #7199 is live. `GRAM_DATABASE_URL` must point at the primary. Stdout carries aggregate counts only; ids and scopes go to the `-report` file, which is created `0600` and never overwritten. Keep it somewhere durable. The report is written when the run exits, including on an error, interrupt, `SIGTERM` or `SIGHUP`; after a `SIGKILL` or crash it is empty, and the backfill's audit entries remain the record (see Restore).

Writes require `-confirm-environment` matching `-environment` and `-confirm-target=<host>:<port>/<database>` matching `GRAM_DATABASE_URL`; a mismatch prints the expected target.

Pass `-exclude-organization=<ORG_ID>` (repeatable) to leave an organization untouched: its clients report `skip_excluded`, and no row or audit entry is written for it in any mode, restore included. Keep the ids in the ticket, not in this repo, and pass the same set to every run.

## Preview (default)

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations client-scopes \
  -environment=prod \
  -created-before=<RFC 3339 time #7199 reached the environment> \
  -exclude-organization=<ORG_ID> \
  -report=<path>/client-scopes-preview.json
```

## Apply

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations client-scopes \
  -apply \
  -environment=prod \
  -confirm-environment=prod \
  -confirm-target=<host>:<port>/<database> \
  -created-before=<same value as the preview> \
  -exclude-organization=<ORG_ID> \
  -report=<path>/client-scopes-apply.json
```

Each client is cleared in its own transaction with a 5s lock timeout: the row is locked, the evidence is rechecked, the update compares `updated_at`, and a `remote-session-client:update` audit entry with actor `system:client-scopes-backfill` records both snapshots. A client that is gone or locked is reported as `skip_changed`. The write bumps `updated_at`, so a registration rotation already in flight for that client retries on its next login; run off-peak. A rerun finds nothing to clear.

## Restore

The backfill's audit entries are the restore record, so a lost report does not block it. `-restore` puts back every scope this backfill cleared that is still unset, with its own audit entry; a scope set since is left alone.

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations client-scopes \
  -restore -apply \
  -environment=prod \
  -confirm-environment=prod \
  -confirm-target=<host>:<port>/<database> \
  -exclude-organization=<ORG_ID> \
  -report=<path>/client-scopes-restore.json
```

Without `-apply` it lists what it would restore.
