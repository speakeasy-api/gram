# Legacy alias endpoint backfill

This offline application-data backfill gives each hosted MCP server an explicit `mcp_endpoints` row for every address its toolset's legacy alias (`toolsets.mcp_slug` / `toolsets.custom_domain_id`) still serves and no live endpoint holds. It must be applied before the alias and its `/mcp/{slug}` fallback are removed, so stored client URLs keep working and the slugs stay reserved. It does not alter the schema.

Run from `server/`. `GRAM_DATABASE_URL` must point directly at the intended PostgreSQL primary: a dry run takes the same locks and rolls back, so it cannot run on a read replica.

## Which addresses

For each toolset with an alias, on its canonical server (`mcp_servers.id = toolsets.id`):

- **The alias's own address**: the platform host or its custom domain, with the alias slug.
- **The platform address of a custom-domain alias**: the platform host also serves a custom-domain alias when no live platform alias has the slug, no other live-project custom-domain alias has it, and the organization's custom domain has no IP allowlist (lockdown).

Every address goes through the hosted endpoint create path's rules: the slug is at most 40 characters, a custom-domain address must be on the organization's verified and activated domain, and a server whose network mode is not `public_only` needs a live private ingress covering all its endpoints. A rule that rejects an address is reported as a skip with its reason. The one exception is the organization slug prefix the create path requires on platform slugs (and on every slug of a free account): the alias already serves each address, so the backfill keeps its slug as clients hold it. Endpoints created through the API still need the prefix, and an endpoint saved without changing its address is not checked for it again.

## Preview (default)

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations legacy-alias-endpoints
```

Scope a run with `-organization=<ORG_ID>` or `-project=<PROJECT_ID>`. Each server runs in its own transaction, which is always rolled back in a preview.

## Apply

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations legacy-alias-endpoints \
  -dry-run=false \
  -journal=/secure/path/legacy-alias-endpoints-<DATE>.jsonl \
  -confirm-target=db.example.internal:5432/gram
```

`-confirm-target` must exactly match the host, port, and database parsed from `GRAM_DATABASE_URL`. `-journal` must name a new file; it is created with mode 0600 and receives one JSON object per created endpoint (`endpoint_id`, `mcp_server_id`, `project_id`, `slug`, `custom_domain_id`), flushed after each server commits.

Each server's transaction takes the hosted endpoint create path's locks in its order: the organization's network ingress lock and the project's admission lock, the toolset row, the custom domain row, the server row, then each address's slug scope lock. Each created endpoint gets an `mcp-endpoint:create` audit row by `system:legacy-alias-endpoint-backfill`; no webhook events are published. The backfill does not attach servers to the default plugin or request plugin publication. When it creates an endpoint, the alias then follows the server's endpoints by the same rule every endpoint write applies, which leaves it in place while an endpoint holds its address.

The run is idempotent: a rerun reports every address it created as `already_held` and writes nothing.

## Output

One line per address, then totals:

`toolset=… project=… address=platform/<slug>|domain:<id>/<slug> platform_for_custom_domain=… action=… endpoint=… reason="…"`

`mode=… already_held=N conflict=N created=N|would_create=N skip=N failed=N`

- `would_create` / `created`: the endpoint the run adds.
- `already_held`: the canonical server already has an endpoint at the address.
- `conflict`: another server's endpoint, or another toolset's alias, holds the address. It is never taken; the alias does not serve that address today either.
- `skip`: the address is not served (toolset, project or server deleted; custom domain deleted; platform host not serving a custom-domain alias) or a hosted address rule rejects it. Review every rule-based skip before the alias is removed: its URL stops working then.
- `failed`: the server's transaction failed and rolled back. Rerun; the command exits non-zero while any server fails.

## Rollback

Remove the endpoints listed in the journal. Run it in a transaction with no apply running, and preview first:

```sql
BEGIN;

SELECT count(*)
FROM mcp_endpoints
WHERE id = ANY('{<ENDPOINT_ID>,<ENDPOINT_ID>}'::uuid[])
  AND deleted IS FALSE;

UPDATE mcp_endpoints
SET deleted_at = clock_timestamp(), is_domain_root = NULL
WHERE id = ANY('{<ENDPOINT_ID>,<ENDPOINT_ID>}'::uuid[])
  AND deleted IS FALSE;

COMMIT;
```

The backfill moves an alias only when no endpoint on a live domain holds the alias's address, as for a custom-domain alias on a deleted domain; the alias then moves to the new platform endpoint, an address the platform host already served it at. A rollback leaves it there, where the fallback still serves it.
