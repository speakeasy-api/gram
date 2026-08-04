# wraptoolsets migration

Wraps every live toolset that still publishes directly through the
`toolsets.mcp_slug` column in the canonical publishing model:

- one toolset-backed `mcp_servers` row (`toolset_id` set; remote/tunneled
  backends, user-session issuer, and variations group left NULL — auth and
  tool definitions stay on the toolset);
- one `mcp_endpoints` row whose `slug` and `custom_domain_id` preserve the
  toolset's published address verbatim (the service-layer org-prefix slug
  validation deliberately does not apply to this repo-layer write);
- `mcp_metadata` and `organization_mcp_collection_server_attachments`
  ownership moved in place from `toolset_id` to `mcp_server_id`, preserving
  row ids, timestamps, `published_by`, and soft-deletion state.

`plugin_servers` and `assistant_toolsets` are deliberately untouched; they
move in a later phase.

## Invocation

The command connects with `$GRAM_DATABASE_URL` (environment only, never a
flag, so credentials do not leak through argv):

```bash
GRAM_DATABASE_URL=postgres://USER:PASS@127.0.0.1:5432/gram \
  go run ./server/cmd/tools/migrations wraptoolsets \
  -report /tmp/wraptoolsets-dry-run.json
```

## Flags

| Flag                 | Default | Meaning                                                                                                                                                                                                               |
| -------------------- | ------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `-dry-run`           | `true`  | Run every read and guard but write nothing; rows that would be wrapped report `would_create`. Pass `-dry-run=false` to apply.                                                                                         |
| `-after`             | (none)  | Resume the keyset scan strictly after this toolset id (uuid).                                                                                                                                                         |
| `-limit`             | `0`     | Maximum candidates processed this run; `0` processes all.                                                                                                                                                             |
| `-project-id`        | (none)  | Restrict the candidate set to one project (canary runs).                                                                                                                                                              |
| `-clear-dead-domain` | `false` | When a candidate's `custom_domain_id` references a soft-deleted domain, null it and wrap the toolset as a platform candidate — preserving the only URL that still resolves.                                           |
| `-move-dependents`   | `false` | Move `mcp_metadata` and collection attachment ownership onto the wrapper. Run only after the release whose collections/metadata APIs read server-keyed rows is deployed — moving earlier orphans toolset-keyed reads. |
| `-report`            | (none)  | Path to write the JSON report.                                                                                                                                                                                        |

## Behavior

Candidates are live toolsets (`deleted IS FALSE`) with `mcp_slug IS NOT NULL`
in live projects, ordered by id. Each candidate is handled in one short
transaction that first takes a fixed advisory lock (serializing concurrent
runs), locks the toolset row with `SELECT ... FOR UPDATE`, and re-checks every
guard against the locked state. A guard failure rolls back and reports a
structured outcome; nothing is partially written.

Wrapper and endpoint ids are UUIDv5 values derived from a fixed namespace and
the toolset id, and the internal `mcp_servers.slug` is the toolset slug plus
the first 8 hex chars of the toolset id. Reruns therefore find their own rows
deterministically: a second apply run performs zero writes and reports every
previously wrapped row as `already_complete`.

`mcp_servers.visibility` is mapped from the toolset flags: `mcp_enabled =
false` always wins as `disabled`; otherwise `mcp_is_public` picks `public` or
`private`. `mcp_servers.environment_id` is resolved from
`(project_id, default_environment_slug)`.

## Outcomes

| Outcome                      | Meaning                                                                                                                                               |
| ---------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| `created`                    | Wrapper server + endpoint inserted; dependent metadata/attachments moved.                                                                             |
| `would_create`               | Dry-run equivalent of `created`.                                                                                                                      |
| `already_complete`           | Exactly one live wrapper already owns the exact endpoint and matches every invariant; nothing (new) to write.                                         |
| `blocked_collision`          | The endpoint address or the derived internal server slug is occupied by an unrelated row. Manual review; the command never renames or repoints.       |
| `blocked_environment`        | `default_environment_slug` does not resolve to a live environment in the project.                                                                     |
| `blocked_dead_domain`        | `custom_domain_id` references a missing, cross-organization, or soft-deleted domain row (the soft-deleted case is unblocked by `-clear-dead-domain`). |
| `blocked_ambiguous_wrapper`  | The toolset has live wrapper state the command cannot safely adopt: multiple wrappers, a wrapper without the exact endpoint, or invariant mismatches. |
| `blocked_dependent_conflict` | A row already exists at a derived id without matching, both toolset and wrapper own metadata, or a collection holds live attachments to both sides.   |
| `blocked_changed`            | The toolset or its project drifted between candidate listing and the locked re-read. Rerun once the state settles.                                    |

The JSON report contains per-outcome counts and per-row entries (toolset id,
project id, published slug, outcome, reason, derived ids, moved-row counts).
It contains ids and slugs only — never organization names or emails.

## Run order

1. **Dry run** the full candidate set and save the report:
   `wraptoolsets -report dry-run.json`
2. **Review** the report: every row should be `would_create` (or
   `already_complete` on later passes). Resolve or explicitly accept every
   `blocked_*` row before applying; decide whether `-clear-dead-domain` is
   approved for the soft-deleted-domain rows.
3. **Apply**, canary first: `wraptoolsets -dry-run=false -project-id <uuid>`
   for a canary project, then `wraptoolsets -dry-run=false` (optionally in
   bounded batches with `-limit` and `-after` from the previous report's
   `last_cursor`).
4. **Rerun to verify**: `wraptoolsets -dry-run=false -report verify.json`
   must perform zero writes and report every wrapped row as
   `already_complete`.
5. **After the server-keyed read release is deployed** (collections and
   metadata APIs resolving wrapper-keyed rows), repeat the dry-run → apply →
   verify sequence with `-move-dependents` to move `mcp_metadata` and
   collection attachment ownership onto the wrappers.

Caveats:

- A dry run cannot foresee collisions that the apply run itself creates
  between two candidates (e.g. two `-clear-dead-domain` rows landing on the
  same platform slug). The apply run's transaction-time guards still block the
  loser; re-review and rerun.
- `-after` skips blocked rows too. After fixing blocked rows, rerun without a
  cursor; completed rows are idempotently reported as `already_complete`.
- An interrupted run exits non-zero and prints the `-after` cursor to resume
  from; every committed row remains valid.
