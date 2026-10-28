# Legacy alias endpoint backfill

This offline application-data backfill gives each hosted MCP server an explicit `mcp_endpoints` row for every address its toolset's legacy alias (`toolsets.mcp_slug` / `toolsets.custom_domain_id`) still serves and no live endpoint holds. It must be applied before the alias and its `/mcp/{slug}` fallback are removed, so stored client URLs keep working and the slugs stay reserved. It does not alter the schema.

Run from `server/`, against dev first, then prod. `GRAM_DATABASE_URL` must point at the intended PostgreSQL primary: a preview takes the same locks and rolls back, so it cannot run on a read replica. When connecting through a proxy, `-confirm-target` names the local proxy, so check that the proxy is connected to the intended instance (for example the instance name in the `cloud-sql-proxy` command) before applying.

## Which addresses

For each toolset with an alias, on its canonical server (`mcp_servers.id = toolsets.id`):

- **The alias's own address**: the platform host or its custom domain, with the alias slug.
- **The platform address of a custom-domain alias**: the platform host also serves a custom-domain alias when no live platform alias has the slug, no other live-project custom-domain alias has it, and the organization's custom domain has no IP allowlist (lockdown). This includes aliases whose custom domain was deleted.

Servers the fallback never serves are skipped: deleted toolsets, projects or servers, a server that is not the toolset's canonical server, and a network mode that keeps the server off the public host (`private_only`). A disabled server still gets its endpoints, since the alias serves again when it is enabled.

Every address goes through the hosted endpoint create path's rules: the slug is at most 40 characters, a custom-domain address must be on the organization's verified and activated domain, and a server whose network mode is not `public_only` needs a live private ingress covering all its endpoints. A rule that rejects an address is reported as a skip with its reason. The one exception is the organization slug prefix the create path requires on platform slugs (and on every slug of a free account): the alias already serves each address, so the backfill keeps its slug as clients hold it. Endpoints created through the API still need the prefix, and an endpoint saved without changing its address is not checked for it again.

## Preview (default)

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations legacy-alias-endpoints \
  -environment=dev
```

Scope a run with `-organization=<ORG_ID>` or `-project=<PROJECT_ID>`. Each server runs in its own transaction, which is always rolled back in a preview.

## Apply

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations legacy-alias-endpoints \
  -apply \
  -environment=dev \
  -confirm-environment=dev \
  -confirm-target=127.0.0.1:5432/gram \
  -confirm-apply=legacy-alias-endpoints \
  -journal=/secure/path/legacy-alias-endpoints-<ENVIRONMENT>-<DATE>.jsonl
```

`-confirm-target` must exactly match the host, port, and database parsed from `GRAM_DATABASE_URL`. `-journal` must name a new file; it is created with mode 0600. For each server that writes, the run appends and flushes a `pending` entry with the full intent before committing, then an outcome entry once the commit returns. If the pending entry cannot be written, the server is rolled back and the run stops.

- `{"status":"pending","mcp_server_id":…,"project_id":…,"endpoints":[{"endpoint_id":…,"address":{"slug":…,"custom_domain_id":…}}],"alias_move":{"from":{…},"to":{…}}}`
- `{"status":"committed|rolled_back|commit_outcome_unknown","mcp_server_id":…,"project_id":…}`

Each server's transaction takes the hosted endpoint create path's locks in its order: the organization's network ingress lock and the project's admission lock, the toolset row, the custom domain rows (the alias's domain and the organization's domain, so the lockdown read is locked), the server row, then each address's slug scope lock. Each created endpoint gets an `mcp-endpoint:create` audit row and each alias move a `toolset:move_legacy_alias` audit row (before and after snapshots hold the old and new alias address), both by `system:legacy-alias-endpoint-backfill` and in the same transaction; no webhook events are published. The backfill does not attach servers to the default plugin or request plugin publication.

After creating endpoints, the alias follows the server's endpoints by the rule every endpoint write applies, but only when its own address now has an endpoint or its custom domain was deleted. In practice it moves only for an alias on a deleted domain, onto the new platform endpoint, which the platform host already served it at. It never leaves a pending, unverified, foreign or ingress-rejected custom domain. Every move is printed, journaled and audited.

The run is idempotent: a rerun reports every address it created as `already_held` and writes nothing.

## Output

One line per address, then totals:

`toolset=… project=… address=platform/<slug>|domain:<id>/<slug> platform_for_custom_domain=… action=… endpoint=… reason="…"`

`toolset=… project=… action=alias_moved|would_move_alias alias_from=… alias_to=…`

`mode=… environment=… already_held=N alias_moved=N conflict=N created=N|would_create=N skip=N failed=N commit_outcome_unknown=N`

- `would_create` / `created`: the endpoint the run adds.
- `already_held`: the canonical server already has an endpoint at the address.
- `conflict`: another server's endpoint, or another toolset's alias, holds the address. It is never taken; the alias does not serve that address today either.
- `skip`: the address is not served, or a hosted address rule rejects it. Review every rule-based skip before the alias is removed: its URL stops working then. Skips ending in "rerun" mean the toolset changed during the run.
- `failed`: the server's transaction failed and rolled back. Rerun.
- `commit_outcome_unknown`: the commit failed or was interrupted, so the server's writes may or may not have landed. Its journal entries are `pending` followed by `commit_outcome_unknown`. Reconcile them as described under Rollback, then rerun.

The command exits non-zero while any server failed or has an unknown commit outcome.

## Verify

After applying:

1. A preview reports no `would_create`.
2. Request a sample of the created addresses, especially platform addresses created for custom-domain aliases, and check they answer as before (for example `curl -si https://<PLATFORM_HOST>/mcp/<SLUG>` returns the same status as before the run, not 404).
3. The legacy slug fallback metric and logs (`mcp.toolset_slug_fallback`) stop showing the affected slugs once clients hit the new endpoints.

## Rollback

Use the journal. Group entries by `mcp_server_id`. A `pending` entry followed by `committed` landed; one followed by `rolled_back` did not. A `pending` entry with no outcome (the run crashed) or followed by `commit_outcome_unknown` must be reconciled against the database: its writes landed if the endpoint ids exist in `mcp_endpoints` and audit rows by `system:legacy-alias-endpoint-backfill` name them (`mcp-endpoint:create` with that endpoint id as subject, `toolset:move_legacy_alias` with the server id as subject). The audit rows alone also reconstruct every write, including an alias's old address.

Before removing anything, check that no later `mcp-endpoint:update` or `mcp-endpoint:delete` audit event names a journaled endpoint; leave any that a user has changed since.

For each endpoint of a landed `pending` entry, remove it only while it still has the journaled address:

```sql
BEGIN;

UPDATE mcp_endpoints
SET deleted_at = clock_timestamp(), is_domain_root = NULL
WHERE id = '<ENDPOINT_ID>'
  AND project_id = '<PROJECT_ID>'
  AND mcp_server_id = '<MCP_SERVER_ID>'
  AND slug = '<SLUG>'
  AND custom_domain_id IS NOT DISTINCT FROM <CUSTOM_DOMAIN_ID_OR_NULL>::uuid
  AND deleted IS FALSE;

COMMIT;
```

For each `alias_move` of a landed `pending` entry, restore the alias only while it still has the journaled `to` address:

```sql
UPDATE toolsets
SET mcp_slug = '<FROM_SLUG>', custom_domain_id = <FROM_CUSTOM_DOMAIN_ID_OR_NULL>::uuid, updated_at = clock_timestamp()
WHERE id = '<MCP_SERVER_ID>'
  AND project_id = '<PROJECT_ID>'
  AND mcp_slug = '<TO_SLUG>'
  AND custom_domain_id IS NOT DISTINCT FROM <TO_CUSTOM_DOMAIN_ID_OR_NULL>::uuid
  AND deleted IS FALSE;
```

The rollback writes no `mcp-endpoint:delete` audit rows; record it in the ticket instead.
