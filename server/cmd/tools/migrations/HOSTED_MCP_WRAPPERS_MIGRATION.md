# Hosted MCP wrappers backfill

Gives every live hosted (toolset-backed) MCP server its `mcp_servers` wrapper and `mcp_endpoints` rows and copies toolset-keyed `mcp` grants onto the wrapper. Two later phases move the toolset-keyed dependents and retire the toolset-keyed grants. Postgres only; connects with `$GRAM_DATABASE_URL`.

```
go run ./server/cmd/tools/migrations hosted-mcp-wrappers -h
```

Every run is a dry run unless `-apply` is passed. `-apply` also requires `-acknowledge-mirror-deployed`.

## Precondition

The toolset↔wrapper mirror (AIS-635) must be deployed before `-apply`. Once a wrapper and endpoint exist, serving is governed by the wrapper; without the mirror, the dashboard's toolset toggles (disable, public/private) stop affecting what the endpoint serves.

## Phases

1. **wrappers** (default). Per toolset: wrapper (UUIDv5 from the toolset id, or an existing live wrapper adopted and reconciled), endpoints carrying `mcp_slug` verbatim, grants copied. Toolset-keyed grants stay in place, so every reader still keyed on the toolset id keeps working.
2. **`-move-dependents`**. Re-keys `mcp_metadata`, collection attachments, `plugin_servers`, and `assistant_toolsets` (into `assistant_mcp_servers`) onto existing wrappers. Run only after the readers of those tables resolve server-keyed rows (AIS-638). Rows whose server-keyed twin already exists are skipped and left in place.
3. **`-retire-toolset-grants`**. Deletes toolset-keyed `mcp` grants that have a wrapper-keyed twin. Run only after every grant reader keys hosted servers on the wrapper id (AIS-637/AIS-638).

## Outcomes

`would_create`/`created`, `would_adopt_existing_wrapper`/`adopted_existing_wrapper` (adoption reconciles issuer, variation group, and visibility to the toolset so the mirror's next write is a no-op; the wrapper keeps its own name and slug, as the mirror only syncs those on a toolset rename; moves a backfill endpoint whose address changed, and clears domain roots when the wrapper becomes disabled; cleared domain ids are reported), `already_complete`, `blocked_collision` (address or wrapper slug held elsewhere, or a platform slug owned by another toolset), `blocked_drift` (toolset no longer qualifies, custom domain not in the organization, or a backfill row was deleted or belongs to another server), `blocked_no_wrapper`, `would_move_dependents`/`moved_dependents`/`skipped_dependents`, `would_retire_toolset_grants`/`retired_toolset_grants`.

A toolset bound to a soft-deleted custom domain gets an endpoint tombstoned at the domain's `deleted_at`. `-aliases slug@custom_domain_id,...` adds a platform-scope twin for the listed custom-domain toolsets; the allowlist lives in the ticket, not the repo. `oauth_proxy_server_id` is ignored; its counts are in every report.

A dry run cannot see an endpoint collision between two candidates that would both be inserted in the same run; those surface under `-apply` as `blocked_collision`. Collisions with existing rows and with live toolsets are reported by the dry run.

## Run book

Every `-apply` below also carries `-acknowledge-mirror-deployed`. The later phases gate on their own readers: `-apply -move-dependents` also needs `-acknowledge-dependent-readers-deployed` (AIS-638), and `-apply -retire-toolset-grants` also needs `-acknowledge-grant-readers-deployed` (AIS-637/AIS-638). Dry runs need none of them.

1. Dev: dry run, then `-apply`; rerun and confirm every row reports `already_complete`.
2. Prod: dry run with `-report`; review outcome counts and every `blocked_*` row.
3. Prod: `-apply -project <canary>`; verify the canary serves through its endpoint and `mcp.toolset_slug_fallback` drops for it.
4. Prod: `-apply` in batches (`-limit`, resume with `-cursor` from the summary's `last_cursor`), then a final unfiltered `-apply` that writes nothing.
5. After AIS-638 deploys: repeat 1–4 with `-move-dependents`.
6. After AIS-637/AIS-638 deploy: repeat 1–4 with `-retire-toolset-grants`.

Failed runs exit nonzero; rows commit one toolset at a time, and an `-apply` run prints the resume cursor (a dry run commits nothing, so its cursor is never reused). Stdout carries the mode, phase, outcome counts, the last toolset id processed, and the report path; the `-report` file holds ids and slugs, never names or emails.
