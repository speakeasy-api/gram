# Hosted MCP wrappers backfill

Gives every live hosted toolset (non-empty `mcp_slug`) created before eager wrapper creation its canonical wrapper: an `mcp_servers` row with `id = toolset.id` and `toolset_id = toolset.id`, plus exactly one `mcp_endpoints` row at the toolset's address. Each row is written by `hostedmcp.Sync`, the same code every toolset write path runs, so a backfilled wrapper is identical to one the live sync would produce. Postgres only; connects with `$GRAM_DATABASE_URL`.

```
go run ./server/cmd/tools/migrations hosted-mcp-wrappers -h
```

Flags: `-apply` (default is a dry run), `-project <PROJECT_ID>`, `-limit N`, `-cursor <TOOLSET_ID>`, `-report <path>`, `-move-dependents`, `-readers-cut-over`.

## Precondition

#7294 and #7298 must be deployed before `-apply`: serving resolves the canonical wrapper first and denies one that drifts from its toolset, and every toolset write keeps the wrapper in sync. Without them a backfilled wrapper would go stale on the next toolset edit.

## Behaviour

Candidates are walked in toolset id order. Each toolset runs in its own transaction: a per-toolset advisory lock, the toolset row `FOR UPDATE` (the lock every `hostedmcp.Sync` caller holds; Sync then locks custom domains, endpoints, and the server in that order), the guards re-checked, then Sync. A dry run executes the same transaction and always rolls it back, so it briefly holds the same row locks but commits nothing.

Writes are audited as the system principal `system:hosted-mcp-wrapper-backfill`. Audit rows are recorded without publishing audit webhook events to the outbox. A transient database error (deadlock, lock or statement timeout, cancellation, lost connection) stops the run instead of being reported as a Sync refusal; resume with `-cursor`. The toolset's `mcp_slug` is used verbatim, including platform slugs that predate the org-prefix rule; neither Sync nor the address check rewrites or rejects them.

Toolset-backed `mcp_servers` rows with a fresh id (gateway members, create-from-source, Platform MCP) are separate servers. They are never adopted, modified, or deleted; they are only counted.

## Outcomes

| Outcome                          | Meaning                                                                                                                                                            |
| -------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `would_create` / `created`       | No canonical wrapper existed; wrapper and endpoint created.                                                                                                        |
| `would_reconcile` / `reconciled` | Canonical wrapper existed but drifted from the toolset (name, slug, visibility, issuer, variations group, or endpoint address); Sync fixed it.                     |
| `already_complete`               | Canonical wrapper and its single matching endpoint exist; no writes.                                                                                               |
| `dead_custom_domain`             | Toolset is bound to a soft-deleted custom domain: wrapper synced, no live endpoint (the domain took its endpoints with it). `wrote` says whether anything changed. |
| `blocked_multiple_endpoints`     | Canonical wrapper has more than one live endpoint (can predate the AIM-485 root-mapping fix); left untouched.                                                      |
| `blocked_slug_collision`         | Another server holds the endpoint address or the project-scoped server slug.                                                                                       |
| `blocked_canonical_conflict`     | The toolset's id is held by a tombstoned or foreign `mcp_servers` row.                                                                                             |
| `blocked_sync_rejected`          | Sync refused the toolset (for example private network access without an online ingress); the reason is Sync's message.                                             |
| `skipped`                        | The toolset was deleted, lost its slug, or changed concurrently since listing; rerun.                                                                              |

Every row also carries `fresh_id_servers` (other live toolset-backed servers for the toolset), and the summary counts `fresh_id_servers_present`. Rows list `domains_to_reconcile` when Sync cleared a domain root; start a custom domain reconcile for each.

Stdout carries the mode, phase, outcome counts, rows scanned, toolsets walked, writes, and the last toolset id processed. The `-report` file holds ids, slugs, outcomes, reasons, and counts; it never holds names or emails.

## Run book

1. Dev: dry run with `-report`; review counts.
2. Dev: `-apply`; rerun `-apply` and confirm `writes` is 0 and every row is `already_complete` (or a repeated `dead_custom_domain` / `blocked_*`).
3. Prod: dry run with `-report`. Review every `blocked_*` row and the `fresh_id_servers_present` count before going on.
4. Prod canary: `-apply -project <PROJECT_ID>`; confirm the project's hosted servers still serve through their endpoints.
5. Prod: `-apply -limit N` in batches, resuming with `-cursor` from the summary's `last_cursor`.
6. Prod: a final unfiltered `-apply` must report `writes: 0`.

Failed runs exit nonzero; rows commit one toolset at a time, and an `-apply` run prints the resume cursor (a dry run commits nothing, so its cursor is never reused).

## Post-run checks

- The `mcp.toolset_slug_fallback` counter drops for traffic to the covered toolsets.
- Plugin publication logs no `toolset_wrapper_ambiguous`.

## Dependent move (`-move-dependents`)

Re-keys the toolset-keyed rows that hang off a hosted toolset onto its canonical wrapper. With `id = toolset.id` there is no id translation.

| Table                | Move                                                                                                                         |
| -------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| `mcp_metadata`       | In place: `toolset_id` cleared, `mcp_server_id` set. Same row id, so `mcp_environment_configs` stay attached.                |
| `plugin_servers`     | In place, live rows only. Same row id, so platform distributions referencing the member keep it. Soft-deleted rows are left. |
| `assistant_toolsets` | Moved to `assistant_mcp_servers` with the same id, environment and `created_at`; the `assistant_toolsets` row is removed.    |

Candidates are walked in toolset id order; `-cursor` and `-limit` count toolsets (the summary's `toolsets`), while `scanned` and `writes` count dependent rows. Each dependent row runs in its own transaction, locking in the writers' order: the wrapper backfill's advisory lock, the toolset row, the assistant (for an attachment), the dependent row, then the wrapper `FOR SHARE`. Metadata and plugin member moves are audited as `system:hosted-mcp-dependent-move` without webhook events (the metadata update carries full before/after snapshots; the plugin member update carries `mcp_server_urn` and `previous_toolset_urn`); assistant attachment changes are not audited on any path. Report rows carry `table` and `row_id`.

| Outcome                | Meaning                                                                                                                                                                 |
| ---------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `would_move` / `moved` | Row re-keyed onto the wrapper.                                                                                                                                          |
| `blocked_twin`         | A server-keyed row for the wrapper already exists (metadata, plugin member, or assistant attachment); untouched.                                                        |
| `blocked_no_wrapper`   | No live canonical wrapper: run the wrapper phase first, unless the reason says the toolset has no `mcp_slug` (it never gets one) or is not in the row's project.        |
| `blocked_unattachable` | Assistant attachment whose wrapper is disabled or has no Speakeasy-hosted endpoint; the assistant write path rejects such servers, so moving it would block every save. |
| `orphaned`             | The toolset or its project is deleted; the row is left as is.                                                                                                           |
| `skipped`              | The row changed concurrently; rerun.                                                                                                                                    |

A rerun after a full apply scans only the rows left behind (`orphaned`, `blocked_*`) and writes nothing, provided no toolset-keyed writer is still live (see below).

### Precondition: AIM-16 deployed

`-move-dependents -apply` refuses to run without `-readers-cut-over` and `-report`. The flag is the operator's confirmation; the command cannot see what is deployed. AIM-16 must cut over both the readers and the toolset-keyed writers, and it cannot be rolled back once a move has been applied.

On main before AIM-16 these readers find only toolset-keyed rows, so a moved row would disappear from them:

- MCP metadata: `getMcpMetadata` by `toolset_slug`, `exportMcpMetadata`, the install page's legacy slug fallback, and the attached-environment and description reads used by MCP serving and instances tool calls (playground, chat, Elements): default environment and system env injection, omitted-variable filtering, server instructions, and legacy header display names (also read by the toolset views).
- Plugins: generated packages lose a moved member's user-header environment configs (`ListPluginEnvironmentConfigsForProject`) and drop it when the wrapper has no usable endpoint; the compatibility reason codes switch to the server set; Platform MCP plugin servers report it as an `mcp_server` backend; the dashboard's plugin banner and plugin detail page match hosted members by `toolsetId` only.
- Assistants: the runtime id changes from the toolset slug to the wrapper slug, and a custom-domain-only toolset is skipped.

These writers create a toolset-keyed twin after a move, which the next run reports as `blocked_twin`, so AIM-16 must cut them over too: `setMcpMetadata` by `toolset_slug`, toolset MCP re-enable attaching to the default plugin, and assistant updates that send the toolset slug.

Unaffected or already server-keyed: plugin membership probes, role delivery, Platform MCP server identities, toolset delete (the wrapper tombstone removes moved rows), and `mcp_server_id` metadata reads. Platform MCP inventory and connection settings start listing moved members. A wrapper rename now reaches a moved member's auto-derived display name through `SyncMcpServerDisplayName`.

### Run book

1. After AIM-16 deploys: dev dry run with `-move-dependents -report`; dev `-apply -readers-cut-over -report`.
2. Prod dry run; review every `blocked_*` and `orphaned` row.
3. Prod canary: `-apply -readers-cut-over -report -project <PROJECT_ID>`.
4. Prod in batches with `-limit N`, resuming each batch with `-cursor` from the previous `last_cursor`; stuck toolsets stay listed, so restarting without a cursor repeats them.
5. A final unfiltered apply must report `writes: 0`.

Keep every `-apply` report: it is the only exact list of moved rows.

### Rollback

Ids are preserved, so a move is reversed by re-keying the reported `row_id`s back (`<ROW_ID>`, `<TOOLSET_ID>` from the report):

```sql
UPDATE mcp_metadata SET mcp_server_id = NULL, toolset_id = '<TOOLSET_ID>' WHERE id = '<ROW_ID>' AND mcp_server_id = '<TOOLSET_ID>';
UPDATE plugin_servers SET mcp_server_id = NULL, toolset_id = '<TOOLSET_ID>' WHERE id = '<ROW_ID>' AND mcp_server_id = '<TOOLSET_ID>';
WITH moved AS (
  DELETE FROM assistant_mcp_servers WHERE id = '<ROW_ID>' AND mcp_server_id = '<TOOLSET_ID>'
  RETURNING id, assistant_id, environment_id, project_id, created_at
)
INSERT INTO assistant_toolsets (id, assistant_id, toolset_id, environment_id, project_id, created_at)
SELECT id, assistant_id, '<TOOLSET_ID>', environment_id, project_id, created_at FROM moved;
```

A reversal fails on a unique index if a toolset-keyed twin was written since; resolve that row first.
