# Registry server versions: staged rollout

This is an operator checklist, not deployment authorization. Agents run only
local disposable tests; no deployed database or configured application database
is a migration test target. Use the approved operator connection and normal
review/deployment safeguards for any eventual rollout.

## Pre-freeze name preflight (read-only prerequisite)

Before freezing writers, run this read-only check through the approved operator
connection against the explicitly verified target. Missing, null, empty, and
non-string names all count as invalid:

```sql
SELECT count(*) AS invalid_server_names
FROM public.mcp_registry_entries
WHERE NOT COALESCE(
  jsonb_typeof(data #> '{server,name}') = 'string'
  AND data #>> '{server,name}' <> '', false
);
```

Require **zero** before beginning the freeze. A nonzero result blocks the rollout:
agree and complete an explicitly approved reconciliation **before the freeze**,
then repeat this check. This runbook authorizes no destructive repair, deletion,
replacement of IDs, or automatic rewriting of historical records. Do not begin
the freeze hoping the backfill will repair invalid names; it intentionally aborts.
The preflight does not replace the backfill's locked validation.

## Release boundaries and continuous write freeze

This release A checkout contains only the generated expand migration and
expand-compatible code. Release B delivers the separately approved contract
migration and final backend capabilities. Never apply contract to unbackfilled data.
Prepare the entire green, approved release stack **before** draining writers.
There are no new flags, application backfill hooks, arbitrary-version fallback,
or temporary `AdoptPublishedDefault` behavior.

1. Ship schema-only expand PR **9076** first:
   `20261113192306_registry_server_identities.sql`. Entry linkage remains nullable
   and name-only uniqueness remains. Package migrations separately from application
   behavior; do not accidentally include the contract migration in this release.
2. Pass the **pre-freeze name preflight** below with zero invalid names before
   beginning one continuous registry-write freeze using the approved operational
   procedure. Drain **all** registry writers and their in-flight work: admin
   dashboard/API edits, creates, publish toggles and icons; Admin MCP registry
   operations; promotion approvals, promotion workers, seeding and other
   registry-producing jobs/integrations. Merely hiding new-version controls is insufficient. Verify
   no queued/retrying or old-process writer can resume. If this cannot be proven,
   stop for an approved rollout adjustment, not an application workaround.
3. Pass the **expand completion gate** below before any backfill. **Directly before A**, execute `server/internal/mcpregistry/backfill.sql` with
   stop-on-error using the approved, explicitly verified target. Validate below.
   This idempotent operator operation is never an application startup hook.
4. Deploy **A**, the version-aware compatible readers and first-server writers,
   built/generated against the actual **nullable expanded schema**. Keep all
   registry writes frozen, including A's otherwise compatible creates/toggles.
   Re-run backfill and validation. Its returned `inserted` count **must be zero**;
   a nonzero count is a stop condition requiring investigation and reconciliation. A must preserve final
   immutable-version and no-auto-default semantics, not introduce transitional
   republish behavior. Nullable SQLc outputs must be generated, not transplanted.
5. **Release B handoff, not executable from this A checkout:** still frozen,
   obtain B's separately approved generated contract schema artifact
   `*_registry_server_versions.sql`. In that B artifact, discover it with `find server/migrations -maxdepth 1 -name '*_registry_server_versions.sql'`;
   require **exactly one** result before applying it. Zero results is expected in A;
   do not create or apply a contract migration from A. The unshipped contract timestamp
   may change on regeneration; the shipped expand filename above remains fixed. Revalidate linkage, default ownership/publication, NOT NULL, removal
   of name-only uniqueness, and validity of exact-version uniqueness. Stop on any
   failure. Do not regenerate a duplicate contract migration.
6. Deploy **B** (final backend/API/Admin MCP version/default/visibility operations)
   and then **C** (admin controls and customer setup snapshot behavior). Keep the
   same freeze through both. Confirm the final controls and setup acceptance tests,
   compatible readers, and every schema/index check below before reopening writes.
7. Lift the freeze **only after the complete final stack is ready** and approved
   operator verification passes. Communicate that this window blocks ordinary
   metadata edits, first-server creation, promotions and publish toggles too.

A missed legacy writer can create unlinked rows before contract. A mistakenly
reopened A unpublish/republish can leave no default and no restoration control.
These are stop conditions: drain again, explicitly reconcile operator decisions,
repeat validation, and retain the freeze. Backfill does not repair existing
parent decisions. Do not treat a passing count alone as proof writers are drained.

## Expand completion gate (before backfill)

Verify the approved connection targets the intended database and that both
unqualified registry tables resolve to `public`. In that same operator session,
use `SET search_path TO public;` for the backfill and validation. Use a direct
connection or a session-scoped pooler connection throughout: a transaction pooler
does not preserve session-level `SET search_path` between transactions. Do not proceed
with missing/invalid indexes or missing/unvalidated/mismatched foreign keys:

```sql
SHOW search_path;
SELECT to_regclass('mcp_registry_entries') = to_regclass('public.mcp_registry_entries')
         AS entries_resolve_public,
       to_regclass('mcp_registry_servers') = to_regclass('public.mcp_registry_servers')
         AS servers_resolve_public;
SELECT c.relname, i.indisvalid, i.indisready, i.indisunique, pg_get_indexdef(i.indexrelid)
FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid
WHERE i.indexrelid IN (to_regclass('public.mcp_registry_entries_server_id_id_key'),
                       to_regclass('public.mcp_registry_servers_name_key'));
SELECT conname, convalidated, condeferrable, condeferred,
       confupdtype, confdeltype, pg_get_constraintdef(oid)
FROM pg_constraint
WHERE (conrelid = 'public.mcp_registry_entries'::regclass
       AND conname = 'mcp_registry_entries_server_id_fkey')
   OR (conrelid = 'public.mcp_registry_servers'::regclass
       AND conname = 'mcp_registry_servers_default_entry_fkey');
```

Require both resolution results true, exactly two index rows (valid, ready and
unique), and the expected `(server_id, id)` and `(name)` definitions on the named
public tables. Require exactly two FK rows with the expected referenced public
tables/columns: entries `server_id → servers(id)` is validated, nondeferrable,
not deferred, update NO ACTION (`a`) and delete SET NULL (`n`); servers
`(id, default_entry_id) → entries(server_id, id)` is validated, deferrable,
initially deferred, and update/delete NO ACTION (`a`/`a`). An interrupted expand
migration can leave counts looking correct but omit the ownership FK; counts are
not evidence that this gate passed. Inspect the complete migration revision state
and retain the freeze until the actual expand has finished.

## Backfill and schema gates

Backfill preserves IDs, JSON (including invalid, missing and null historical
versions), publication flags and timestamps. New parents reuse legacy entry IDs
in a separate table, are visible, and receive a default only for a published
entry. Existing parents/default/visibility decisions are never overwritten.
Historical versions are not normalized; new application writes validate metadata
and SemVer. Discovery uses the explicit default, never an arbitrary version.

Run with stop-on-error and record results at the checkpoints above:

```sql
SELECT count(*) AS unlinked FROM mcp_registry_entries WHERE server_id IS NULL;
SELECT count(*) AS wrong_parent
FROM mcp_registry_entries e LEFT JOIN mcp_registry_servers s ON s.id = e.server_id
WHERE s.id IS NULL OR s.name <> e.data #>> '{server,name}';
SELECT count(*) AS wrong_default
FROM mcp_registry_servers s LEFT JOIN mcp_registry_entries e ON e.id = s.default_entry_id
WHERE s.default_entry_id IS NOT NULL
  AND (e.id IS NULL OR e.server_id <> s.id OR NOT e.published);
-- Freeze-only anomaly gate; not a permanent product invariant.
SELECT count(*) AS published_without_default
FROM mcp_registry_servers s
WHERE s.default_entry_id IS NULL
  AND EXISTS (SELECT 1 FROM mcp_registry_entries e
              WHERE e.server_id = s.id AND e.published);
```

During the freeze all counts must be zero. Nonzero `published_without_default`
means a writer ran or a parent was pre-created; stop for explicit operator
reconciliation. Rerunning backfill deliberately preserves such a parent's decision
and does not repair it. After writes reopen this is a legitimate withdrawn-default
state and is **no longer a gate**. A post-A backfill rerun must additionally report
zero inserted parents; otherwise stop even if the invariant counts pass.

On a backfill error, stop and roll back/disconnect the
failed transaction before retrying; never alter historical JSON or replace IDs
to force success. An unpublished legacy default needs explicit operator
reconciliation, not repeated backfill or fallback selection.

Release B only, after applying B's contract artifact:

```sql
SELECT attnotnull FROM pg_attribute
WHERE attrelid = 'public.mcp_registry_entries'::regclass AND attname = 'server_id';
SELECT to_regclass('public.mcp_registry_entries_name_key') IS NULL AS old_index_gone;
SELECT i.indisvalid, i.indisready, i.indisunique, pg_get_indexdef(i.indexrelid)
FROM pg_index i
WHERE i.indexrelid = to_regclass('public.mcp_registry_entries_server_version_key');
```

Require true NOT NULL and old-index removal, exactly one valid/ready/unique index
row, and the expected `(server_id, (data #>> '{server,version}'))` definition.
Disposable tests prove duplicate non-null identifiers fail and distinct versions
succeed. Historical JSON-null and missing versions extract to SQL NULL and remain
unconstrained by ordinary unique-index semantics (NULLs are distinct); new writes
must pass strict application metadata/SemVer validation. This rollout does not add
`NULLS NOT DISTINCT` or normalize historical values. Do not insert synthetic
acceptance records into a deployed registry.

## Partial migration failure and lock risk

These generated migrations use `atlas:txmode none`; concurrent index operations
are intentionally outside a transaction. The contract drops name-only uniqueness
**before** adding NOT NULL and building exact-version uniqueness. Failure may
leave a partially applied migration, including an INVALID concurrent index.
Keep every writer frozen and inspect the actual schema and Atlas revision state;
never assume rollback or blindly replay either whole file (the expand column or
indexes may already exist, and the contract drop may already have succeeded). Only an approved operator may reconcile/resume the failed
migration using the normal migration procedure.

The potentially failed concurrent indexes are exactly
`public.mcp_registry_entries_server_id_id_key` (expand) and
`public.mcp_registry_entries_server_version_key` (contract). Before any index
cleanup, verify the exact schema-qualified index name, its table,
definition, ownership, validity, and whether a build is still active. Only if the
operator establishes that this is the **INVALID index owned by this failed
migration**, with no active build, may they approve dropping that specific index
concurrently and resuming its generated creation step. An absent index needs no
drop; a valid index must not be dropped as recovery. Do not drop unrelated indexes,
use broad cleanup commands, recreate name-only uniqueness over version history,
or rewrite migration files/checksums. Re-run all gates after approved recovery.

Backfill takes write locks on both registry tables. Foreign-key validation and
`SET NOT NULL` can scan while holding strong locks; concurrent index creation also
has resource and waiting costs. **Production table sizes, lock durations and load
have not been measured here.** Local success is not evidence of a safe production
window. Obtain operator sizing/lock-budget approval and stop on unexpected waits.

The composite default ownership FK is the already-approved, exact generator
exception: `FOREIGN KEY (id, default_entry_id) REFERENCES
mcp_registry_entries(server_id, id) ON UPDATE NO ACTION ON DELETE NO ACTION
DEFERRABLE INITIALLY DEFERRED`. Atlas v1.3.4 drops the column subset from
`ON DELETE SET NULL (default_entry_id)`, which would also null the parent primary
key. This exception is specific to that FK, not permission to change other FK
deletion policy. Application deletion clears the pointer in the same transaction
first; nullable entry-to-parent links still use ON DELETE SET NULL. Do not edit
the generated migration to bypass this contract.

## Setup snapshot boundary

Fresh setup selects the current visible published default. Existing installed
configuration is retained; later edits, withdrawals or default changes do not
rewrite its URL/headers/auth selection. In Platform MCP, configuration becomes a
snapshot when `CompleteRegistration` transactionally materializes resources,
**before authentication completes**. Replay of those resources uses persisted
configuration and readiness, with the existing principal/project/lifecycle and
source-only eligibility checks; it must not reshape configuration from the latest
catalogue default. Authorization checks precede side effects.

An empty pending receipt has no materialized configuration to preserve: current
admission (or refusal) still applies. Existing expired-receipt handling takes the
fresh-admission path; do not claim every pending or expired retry is frozen to an
old version. No new receipt/provenance storage is introduced by this rollout.

## Rollback limits

Before contract, an explicit decision to abandon the rollout may revert application
code while retaining expand schema and the freeze. Do not automatically reopen
legacy writes; any later attempt requires drain/backfill/validation again.
After contract, legacy writers omitting linkage are **not** rollback targets.
After multiple versions exist, never restore name-only readers or name-only
uniqueness. Retain version-aware readers, drain writers and repair forward. A is
only a possible frozen bridge if its exact packaged artifact is verified against
the contracted/multi-version schema; its label alone proves nothing. Prefer final
readers with writes stopped. No schema downgrade is promised.

## Local executable verification and remaining gates

In this **release A** checkout,
`mise run test:server ./internal/mcpregistry/ -count=1` includes
`TestExpandMigrationTransition`: it reuses disposable test containers/clones,
creates a private schema, replays the actual legacy and generated expand files,
executes the operator backfill twice, and checks preservation, existing default
choices and zero linkage/default invariant counts. `TestExpandLinkageRejectsInvalidOwnership`
checks the actual expand ownership constraints. Neither test applies contract.

**Release B** must deliver `TestVersionMigrationTransition` with its actual
contract migration and verify NOT NULL, index validity, name-only index removal,
and version uniqueness. B's exact-one migration-discovery gate above must pass
against B's artifact, not this checkout. These are B delivery requirements, not
claims about tests or migration files present in A.

These tests never use the configured application database. Migration lint must
likewise point only at a separately owned disposable database using process-only
overrides.

This test and runbook do not establish whole-feature acceptance. Combined backend,
admin/dashboard tests and typechecks, final browser/persistence acceptance, and
release A/B/C packaging (including nullable A generation and separate migration
artifacts) remain independent gates. No production rollout is authorized here.
