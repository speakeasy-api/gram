# Hosted MCP wrappers backfill

Gives every live hosted toolset (non-empty `mcp_slug`) created before eager wrapper creation its canonical wrapper: an `mcp_servers` row with `id = toolset.id` and `toolset_id = toolset.id`, plus exactly one `mcp_endpoints` row at the toolset's address. Each row is written by `hostedmcp.Sync`, the same code every toolset write path runs, so a backfilled wrapper is identical to one the live sync would produce. Postgres only; connects with `$GRAM_DATABASE_URL`.

```
go run ./server/cmd/tools/migrations hosted-mcp-wrappers -h
```

Flags: `-apply` (default is a dry run), `-project <PROJECT_ID>`, `-limit N`, `-cursor <TOOLSET_ID>`, `-report <path>`, `-move-dependents`, `-readers-cut-over`, `-cleanup-orphans`, `-rekey-removal-records`.

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
5. Prod: `-apply -limit N` in batches, resuming with `-cursor` from the summary's `last_cursor`. Give each batch its own `-report` path: an `-apply` refuses an existing file.
6. Prod: a final unfiltered `-apply` must report `writes: 0`.

Failed runs exit nonzero; rows commit one toolset at a time, and an `-apply` run prints the resume cursor (a dry run commits nothing, so its cursor is never reused).

## Post-run checks

- The `mcp.toolset_slug_fallback` counter drops for traffic to the covered toolsets.
- The AIM-7 removal gate requires zero new increments to `mcp.toolset_slug_fallback` summed over every `gram.mcp.entry_point` value, and to `mcp.legacy_audience_accepted`, for at least the longest access-token lifetime (720h): entry points such as `shared_resource` and `org_gateway_probe` were split out of `oauth`, so no single tag covers all traffic.
- Plugin publication logs no `mcp_server_ambiguous` or "ambiguous toolset MCP serving wrapper".

## Dependent move (`-move-dependents`)

Re-keys the toolset-keyed rows that hang off a hosted toolset onto its canonical wrapper. With `id = toolset.id` there is no id translation.

| Table                | Move                                                                                                                         |
| -------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| `mcp_metadata`       | In place: `toolset_id` cleared, `mcp_server_id` set. Same row id, so `mcp_environment_configs` stay attached.                |
| `plugin_servers`     | In place, live rows only. Same row id, so platform distributions referencing the member keep it. Soft-deleted rows are left. |
| `assistant_toolsets` | Moved to `assistant_mcp_servers` with the same id, environment and `created_at`; the `assistant_toolsets` row is removed.    |

Candidates are walked in toolset id order; `-cursor` and `-limit` count toolsets (the summary's `toolsets`), while `scanned` and `writes` count dependent rows. Each dependent row runs in its own transaction with `SET LOCAL lock_timeout` (5s), locking in the writers' order: the wrapper backfill's advisory lock, the toolset row, the assistant (for an attachment), the dependent row, then the wrapper `FOR SHARE`, then any server-keyed twin. A lock timeout stops the run like any other transient error. Metadata and plugin member moves are audited as `system:hosted-mcp-dependent-move` without webhook events (the metadata update carries full before/after snapshots; the plugin member update carries `mcp_server_urn` and `previous_toolset_urn`); assistant attachment changes are not audited on any path. Report rows carry `table` and `row_id`.

### Twins

A twin is a server-keyed row for the same wrapper next to the toolset-keyed one (the same plugin, the same assistant, or the wrapper's metadata). Phase A readers (#7435 plugins, #7436 assistants, #7437 metadata) prefer the toolset-keyed row, so that row is what users see today, and the move keeps it: in the same transaction it snapshots the twin, removes it, then moves the toolset-keyed row with its id, environment, env configs, distributions and `created_at`.

- `mcp_metadata`: the twin is deleted, and its `mcp_environment_configs` cascade with it; the toolset-keyed row's configs win.
- `plugin_servers`: the twin is soft-deleted like a member removal and audited as `plugin:server_remove`. A twin referenced by a platform distribution blocks the row (`blocked_twin`) instead.
- `assistant_mcp_servers`: the twin is deleted; the toolset-keyed attachment's environment wins.

The report and journal rows carry `collapsed_twin`: the twin's table, id and `deletion` (`deleted` or `soft_deleted`). On `-apply` they also carry its full row (`to_jsonb`) and, for metadata, its env configs; a dry-run report omits those.

| Outcome                                               | Meaning                                                                                                                                      |
| ----------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| `would_move` / `moved`                                | Row re-keyed onto the wrapper.                                                                                                               |
| `would_move_collapsing_twin` / `moved_collapsed_twin` | Twin removed, then the row re-keyed; see `collapsed_twin`.                                                                                   |
| `blocked_twin`                                        | The plugin twin is referenced by a platform distribution, or a twin appeared outside the collapse (a concurrent writer); untouched, rerun.   |
| `blocked_no_wrapper`                                  | No live canonical wrapper: run the wrapper phase first, unless the reason says the toolset has no `mcp_slug` or is not in the row's project. |
| `blocked_unattachable`                                | Assistant attachment the assistants reader does not dial (toolset MCP off, or no `mcp_slug`); left until the toolset is dialed again.        |
| `orphaned`                                            | The toolset, its project, or the attachment's assistant is gone from the row's project; the row is left as is.                               |
| `skipped`                                             | The row changed concurrently; rerun.                                                                                                         |
| `commit_unknown`                                      | The commit errored after the server may have committed; the run stops. The journal intent covers the row either way.                         |

Only the three twin unique constraints (`mcp_metadata_mcp_server_id_key`, `plugin_servers_plugin_id_mcp_server_id_key`, `assistant_mcp_servers_assistant_id_mcp_server_id_key`) map to `blocked_twin`; any other unique violation stops the run.

A rerun after a full apply scans only the rows left behind (`orphaned`, `blocked_*`) and writes nothing, provided no toolset-keyed writer is still live (see below).

### Journal

`-move-dependents -apply` writes `<report>.journal.jsonl` next to the report. For each row it appends and fsyncs an `intent` record before the commit, then a `committed` (or `commit_unknown`) record after. Each record carries `project_id`, `toolset_id`, `row_id`, `outcome`, `from` and `to` (table, column, key) and `collapsed_twin`. A crash between the two leaves an `intent` alone: that row may or may not have committed, and the rollback statements are written so that replaying them for a row that never committed changes nothing. An `-apply` refuses an existing report or journal path, so every batch keeps its own pair.

### Precondition: AIM-16 phases A and B deployed

`-move-dependents -apply` refuses to run without `-readers-cut-over` and `-report`. The flag is the operator's confirmation that AIM-16 phase A (dual-key readers: #7435 plugins, #7436 assistants, #7437 metadata) and phase B (writers switched to the server key) are both fully deployed; the command cannot see what is deployed. Phase A keeps writing today's toolset-keyed shape so old pods stay compatible, and phase B flips the writers later, so the move must run after phase B to settle at `writes: 0`.

Assistants phase B is the specific prerequisite: a phase A assistant save writes hosted servers back to `assistant_toolsets`, so it reverses a move. Under #7435 a toolset rename also renames toolset-keyed plugin entries; a moved member follows the wrapper name instead (`SyncMcpServerDisplayName`).

On main before AIM-16 these readers find only toolset-keyed rows, so a moved row would disappear from them:

- MCP metadata: `getMcpMetadata` by `toolset_slug`, `exportMcpMetadata`, the install page's legacy slug fallback, and the attached-environment and description reads used by MCP serving and instances tool calls (playground, chat, Elements): default environment and system env injection, omitted-variable filtering, server instructions, and legacy header display names (also read by the toolset views).
- Plugins: generated packages lose a moved member's user-header environment configs (`ListPluginEnvironmentConfigsForProject`) and drop it when the wrapper has no usable endpoint; the compatibility reason codes switch to the server set; Platform MCP plugin servers report it as an `mcp_server` backend; the dashboard's plugin banner and plugin detail page match hosted members by `toolsetId` only.
- Assistants: the runtime id changes from the toolset slug to the wrapper slug, and a custom-domain-only toolset is skipped.

These writers create a toolset-keyed twin after a move, which the next run collapses, so AIM-16 phase B must switch them to the server key: `setMcpMetadata` by `toolset_slug`, toolset MCP re-enable attaching to the default plugin, and assistant updates that send the toolset slug.

Unaffected or already server-keyed: plugin membership probes, role delivery, Platform MCP server identities, toolset delete (the wrapper tombstone removes moved rows), and `mcp_server_id` metadata reads. Platform MCP inventory and connection settings start listing moved members. A wrapper rename now reaches a moved member's auto-derived display name through `SyncMcpServerDisplayName`.

### Run book

1. After AIM-16 phases A and B are both fully deployed: dev dry run with `-move-dependents -report <path>`; dev `-apply -move-dependents -readers-cut-over -report <new path>`.
2. Prod dry run; review every `blocked_*`, `orphaned` and `would_move_collapsing_twin` row (the twin's contents are discarded).
3. Prod canary on `<PROJECT_ID>`: download every plugin package in the project (`downloadPluginPackage`) and unpack it; `-apply -move-dependents -readers-cut-over -report <path> -project <PROJECT_ID>`; download and unpack again and diff. Any difference stops the rollout; roll the project back from its journal.
4. Prod in batches with `-limit N`, resuming each batch with `-cursor` from the previous `last_cursor` and a new `-report` path; stuck toolsets stay listed, so restarting without a cursor repeats them.
5. A final unfiltered apply must report `writes: 0`.

Keep every `-apply` report and journal: the journals are the exact list of moved rows and removed twins.

### Rollback

Order matters:

1. Roll code back to AIM-16 phase A, never below it: phase A reads both keys, so moved rows stay visible, and phase B writers stop.
2. Roll the data back from the journals, newest batch first, one project at a time.
3. Only then roll below phase A if needed.

Ids are preserved, so every statement is keyed by journal values and guarded on the moved shape, so replaying it for a row that never committed, or twice, changes nothing. Generate the SQL for one project from a journal with this `jq` program (`rollback.jq`), then review and run it in one transaction:

```jq
def q: "'" + (if type == "string" then . else tojson end | gsub("'"; "''")) + "'";
reverse[]
| select(.event == "intent" and .project_id == $p)
| ($p | q) as $proj | (.row_id | q) as $row | (.to.id | q) as $key | .collapsed_twin as $twin
| if .from.table == "mcp_metadata" then
    "UPDATE mcp_metadata SET mcp_server_id = NULL, toolset_id = \($key) WHERE id = \($row) AND project_id = \($proj) AND mcp_server_id = \($key);",
    ($twin // empty
      | "INSERT INTO mcp_metadata SELECT (r).* FROM jsonb_populate_record(NULL::mcp_metadata, \(.row | q)::jsonb) AS r WHERE r.project_id = \($proj) ON CONFLICT (id) DO NOTHING;",
        "INSERT INTO mcp_environment_configs SELECT (c).* FROM jsonb_populate_recordset(NULL::mcp_environment_configs, \(.environment_configs // [] | q)::jsonb) AS c WHERE c.project_id = \($proj) ON CONFLICT (id) DO NOTHING;")
  elif .from.table == "plugin_servers" then
    "UPDATE plugin_servers AS ps SET mcp_server_id = NULL, toolset_id = \($key), updated_at = clock_timestamp() FROM plugins AS pl WHERE ps.id = \($row) AND pl.id = ps.plugin_id AND pl.project_id = \($proj) AND ps.mcp_server_id = \($key) AND ps.deleted IS FALSE;",
    ($twin // empty
      | "UPDATE plugin_servers AS ps SET deleted_at = NULL, updated_at = clock_timestamp() FROM plugins AS pl WHERE ps.id = \(.id | q) AND pl.id = ps.plugin_id AND pl.project_id = \($proj) AND ps.deleted_at IS NOT NULL;")
  else
    "WITH moved AS (DELETE FROM assistant_mcp_servers WHERE id = \($row) AND project_id = \($proj) AND mcp_server_id = \($key) RETURNING id, assistant_id, environment_id, project_id, created_at) INSERT INTO assistant_toolsets (id, assistant_id, toolset_id, environment_id, project_id, created_at) SELECT id, assistant_id, \($key), environment_id, project_id, created_at FROM moved;",
    ($twin // empty
      | "INSERT INTO assistant_mcp_servers SELECT (r).* FROM jsonb_populate_record(NULL::assistant_mcp_servers, \(.row | q)::jsonb) AS r WHERE r.project_id = \($proj) ON CONFLICT (id) DO NOTHING;")
  end
```

```
jq -r -s --arg p <PROJECT_ID> -f rollback.jq <journal> > rollback-<PROJECT_ID>.sql
```

`reverse` replays rows newest first, and within a row the moved row is re-keyed before its twin is restored, so the twin's unique key is free again. A statement that fails on a unique index means a toolset-keyed twin was written after the move; resolve that row by hand.

## Orphan cleanup (`-cleanup-orphans`)

Removes the toolset-keyed dependents the move left as `orphaned`: rows whose toolset, or whose project, is deleted. It walks the same candidates as `-move-dependents`, with the same `-project`, `-cursor`, `-limit` and `-report` conventions, and each row runs in its own transaction with the move's 5s `lock_timeout` and lock order: the advisory lock, project distribution admission (plugin members), the toolset row `FOR NO KEY UPDATE` together with its project row `FOR SHARE` (so a project delete or restore waits until the row commits), the assistant (attachments), the dependent row, then any twin. Under those locks it re-checks that the toolset or project is deleted and that the row is still toolset-keyed, and that no server-keyed twin exists under the toolset's canonical id (`mcp_server_id = toolset.id`: the wrapper's metadata, the same plugin's member, or the same assistant's attachment). A row whose toolset and project are both live is never written.

| Table                | Removal                                                                                                                                                                                                                                                         |
| -------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `mcp_metadata`       | Its `mcp_environment_configs` are locked and imaged, then the row is deleted and they cascade. Audited as `mcp_metadata:update` with a before snapshot and no after: there is no metadata delete action, and an update with an empty after records the removal. |
| `plugin_servers`     | Soft-deleted, as toolset deletion does. Audited as `plugin:server_remove` with `toolset_urn`. In a live project it then requests a plugin publication (see below).                                                                                              |
| `assistant_toolsets` | Deleted, as toolset deletion does; attachment changes are not audited on any path.                                                                                                                                                                              |

Audit rows carry the system principal `system:hosted-mcp-orphan-cleanup` and publish no webhook events.

Plugin publication: with `-plugin-publication-emit` (default `$GRAM_PLUGIN_PUBLICATION_EMIT_ENABLED`, the app's switch) a removed member of a live project enqueues a publication request in the same transaction, as DeleteToolset does; it is written only when the project has a marketplace connection, and the report row says `publication_requested`. A dry run rolls back, so it enqueues nothing. Members of a deleted project request none.

| Outcome                    | Meaning                                                                                                                                                         |
| -------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `would_delete` / `deleted` | Orphan removed; `reason` says whether the toolset or the project is deleted, `removed` names the table, id and `deletion` (`deleted` or `soft_deleted`).        |
| `blocked_no_wrapper`       | "toolset is not in the row's project", or a live toolset without a canonical wrapper (no `mcp_slug`, or the wrapper phase has not run); left for a manual call. |
| `blocked_twin`             | A server-keyed twin exists under the canonical id; left untouched.                                                                                              |
| `blocked_distributed`      | A platform distribution references the plugin member; left untouched.                                                                                           |
| `blocked_foreign_config`   | An env config of the metadata belongs to another project, so the cascade would reach outside the row's project; left untouched.                                 |
| `skipped`                  | The toolset is live (`-move-dependents` moves the row), or the row changed concurrently; rerun.                                                                 |
| `orphaned`                 | The attachment's assistant is not in the row's project; left untouched.                                                                                         |
| `commit_unknown`           | The commit errored after the server may have committed; the run stops. The journal covers the row either way.                                                   |

`-cleanup-orphans -apply` requires `-report` and writes `<report>.journal.jsonl` (mode 0600) like the move: an `intent` record before the commit and a `committed` (or `commit_unknown`) record after. Each record carries `from` (the toolset key) and `removed` with the full row image (`to_jsonb`), the metadata's env configs, and for plugin members the `soft_deleted_at` this tool set. The report, dry run or apply, carries only table, id and deletion; row images live only in the journal.

### Run book

1. Dev: dry run with `-cleanup-orphans -report <path>`; `-apply -cleanup-orphans -report <new path>`; rerun and confirm `writes: 0`.
2. Prod dry run with `-report`; confirm the `would_delete` count and review every `blocked_*` row.
3. Prod `-apply -cleanup-orphans -report <path>`, optionally a canary `-project <PROJECT_ID>` first. Pass `-plugin-publication-emit` to match the app's setting.
4. A rerun must report `writes: 0`.

### Rollback

[`hostedmcpbackfill/restore-orphans.jq`](hostedmcpbackfill/restore-orphans.jq) turns a journal into restore SQL for one project:

```
jq -r -s --arg p <PROJECT_ID> -f server/cmd/tools/migrations/hostedmcpbackfill/restore-orphans.jq <journal> > restore-orphans-<PROJECT_ID>.sql
```

Review it, then run it in one transaction. It replays only rows with a `committed` record, newest first; `commit_unknown` rows, and intents with no outcome record (a crash between the two), are printed as `--` comments to check by hand. Metadata, env configs and attachments are re-inserted from their images with `ON CONFLICT (id) DO NOTHING`, so a replay is a no-op. A plugin member gets back its recorded `deleted_at` and `updated_at`, and only while its `deleted_at` still equals the value this tool set, so a member someone else restored or deleted again is left alone.

Failure modes, each of which stops the transaction for a hand fix:

- `mcp_metadata_toolset_id_key`: the toolset already has new toolset-keyed metadata.
- `assistant_toolsets_assistant_id_toolset_id_key`: the assistant was attached to the toolset again.
- The plugin `display_name` unique index: another live member of the plugin took the name.
- A foreign key: a referenced environment, assistant, plugin or project was hard-deleted since.

`TestRestoreOrphansJQReplaysCommittedRows` runs this program against a real journal.

## Removal record re-key (`-rekey-removal-records`)

When an admin removes a server from a plugin, its `plugin_servers` row is soft-deleted, and role delivery reads that row as "removed, do not re-add" (`HasRoleDeliveryMembership` with `preserve_removal`, on the setup and eligibility paths). Rows removed before the server key carry only `toolset_id`. This mode moves those removal records onto the canonical server key before AIM-581 drops `plugin_servers.toolset_id`: it sets `mcp_server_id = toolset_id` and `toolset_id = NULL` and leaves `deleted_at` as it was. Role delivery's server-key arm counts soft-deleted rows under `preserve_removal`, so a re-keyed record keeps the server out exactly as before.

It walks toolsets that have `plugin_servers` rows with `toolset_id` set and `mcp_server_id` and `meta_mcp_server_id` both NULL, with the same `-project`, `-cursor`, `-limit` and `-report` conventions as the other dependent phases. Each row runs in its own transaction with the 5s `lock_timeout`, taking locks in this order: the advisory lock, project distribution admission (role delivery takes it before its plugin locks), the toolset row `FOR NO KEY UPDATE` with its project `FOR SHARE`, the record `FOR UPDATE`, the canonical server `FOR SHARE`, then any server-keyed removal record of the same plugin. The `UPDATE` matches soft-deleted rows only (`deleted IS TRUE`), so a live member is never written.

Constraints: every unique index on `plugin_servers` that the re-key touches (`plugin_id` with `toolset_id`, `mcp_server_id`, `meta_mcp_server_id` or `display_name`) is partial on `deleted IS FALSE`, so a soft-deleted row is never in it. `plugin_id, id` and the primary key do not change. `backend_exclusivity_check` stays satisfied (exactly one of the three keys), and the `mcp_server_id` foreign key points at the live canonical server.

Nothing visible to users changes: a soft-deleted member is not in any plugin package or listing. So the re-key writes no audit rows and requests no plugin publication. The journal is the record.

| Outcome                     | Meaning                                                                                                                                                                                          |
| --------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `would_rekey` / `rekeyed`   | The removal record now carries the canonical server key; `deleted_at` is unchanged.                                                                                                              |
| `orphaned`                  | The toolset or its project is deleted; `reason` is "toolset deleted" or "project deleted". Left for AIM-581.                                                                                       |
| `blocked_no_wrapper`        | The toolset is not in the plugin's project ("toolset is not in the row's project"), or it is live but has no live canonical server (`id = toolset_id`, same project). Left for AIM-581.         |
| `blocked_duplicate_removal` | The plugin already has a soft-deleted server-keyed row for the canonical server, or an earlier row of this run re-keys one, and that row already blocks the re-add. Left for AIM-581.            |
| `blocked_live`              | A live toolset-keyed member; this mode never touches it.                                                                                                                                         |
| `skipped`                   | The row changed concurrently; rerun.                                                                                                                                                             |
| `commit_unknown`            | The commit errored after the server may have committed; the run stops. The journal covers the row either way.                                                                                    |

The run tracks the (plugin, canonical server) pairs it re-keys, or in a dry run would re-key, so a dry run reports the same `blocked_duplicate_removal` rows as the apply.

`-rekey-removal-records -apply` requires `-report` and writes `<report>.journal.jsonl` (mode 0600): an `intent` record before each commit and a `committed` (or `commit_unknown`) record after. Each record carries `from` (`toolset_id`), `to` (`mcp_server_id`) and `rekeyed`, which holds the row's full image from before the re-key (`to_jsonb`) and its `soft_deleted_at`. The report, whether from a dry run or an apply, carries ids, slugs, outcomes, reasons; never names.

### Run book

Precondition: run after the wrapper phase and `-move-dependents`, and before AIM-581's column drop.

1. Dev: dry run with `-rekey-removal-records -report <path>`, then `-apply -rekey-removal-records -report <new path>`. Rerun and confirm `writes: 0`.
2. Prod: dry run with `-report`. Count the soft-deleted toolset-keyed rows (add `JOIN plugins AS pl ON pl.id = ps.plugin_id` and `pl.project_id = '<PROJECT_ID>'` for a `-project` run):

   ```sql
   SELECT count(*) FROM plugin_servers AS ps
   WHERE ps.deleted IS TRUE AND ps.toolset_id IS NOT NULL AND ps.mcp_server_id IS NULL AND ps.meta_mcp_server_id IS NULL;
   ```

   It must equal the report's row count minus `blocked_live`. Review every `orphaned`, `blocked_no_wrapper` and `blocked_duplicate_removal` row; `would_rekey` is the rest.

3. Prod: `-apply -rekey-removal-records -report <path>`, optionally with a canary `-project <PROJECT_ID>` first. Resume a stopped run with `-cursor <last_cursor>` and a new `-report`, which writes a new journal.
4. A rerun must report `writes: 0`. Rows still listed are the `orphaned`, `blocked_*` and `skipped` ones, which AIM-581 handles.

### Rollback

While `plugin_servers.toolset_id` still exists, [`hostedmcpbackfill/restore-removal-records.jq`](hostedmcpbackfill/restore-removal-records.jq) turns a journal into restore SQL for one project. A run spans many projects, and a resumed run writes one journal per batch, so generate a file for every project of every journal. The files hold row images, so keep them 0600 and outside the repo, next to their journal:

```
umask 077
for journal in <newest journal> ... <oldest journal>; do
  for p in $(jq -r -s 'map(.project_id) | unique[]' "$journal"); do
    jq -r -s --arg p "$p" -f server/cmd/tools/migrations/hostedmcpbackfill/restore-removal-records.jq "$journal" > "$journal.restore-$p.sql"
  done
done
```

Review each file, then run each in its own transaction (`psql -v ON_ERROR_STOP=1 --single-transaction -f <file>`), replaying every journal from every resumed batch, newest batch first. Each file replays only rows with a `committed` record, newest first. `commit_unknown` rows, and intents with no outcome record, are printed as `--` comments to check by hand. Each statement puts the row back under its toolset key, along with its recorded `updated_at`. It applies only while the row is still server-keyed and its `deleted_at` matches the journal, so a replay is a no-op and a row someone changed since is left alone. `TestRestoreRemovalRecordsJQReplaysCommittedRows` runs this program against a real journal.
