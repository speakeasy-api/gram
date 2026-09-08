# legacy-policy-scope migration

Folds the legacy policy-level risk scope (`risk_policies.message_types`,
`scope_include`, `scope_exempt`) into per-category detection scopes in
`analyzer_config`, then clears the legacy columns. PostgreSQL only.

Tracked on [AIS-678](https://linear.app/speakeasy/issue/AIS-678).

## Why

A risk policy had two scoping surfaces and the scanner intersected them: a
message is scanned for a category only when the policy scope admits it AND the
category scope admits it (`risk_analysis.CategoryScope.InScope`). The policy
editor stopped editing the legacy surface, so policies narrowed by it could not
be widened from the dashboard, and the Policy Center list contradicted the
editor. This migration leaves detection scopes as the only scoping surface.

## Fold rule

Both surfaces narrow, so dropping the legacy scope widens what a policy scans.
The fold is therefore conditional on what the policy does on a match:

| Policy action                 | Fold        | Effect                                                              |
| ----------------------------- | ----------- | ------------------------------------------------------------------- |
| `warn`, `block`, `quarantine` | `preserved` | Legacy scope composed into each category scope. Scanning identical. |
| `flag`                        | `cleared`   | Legacy scope dropped. Scanning widens; only produces more findings. |
| no legacy scope               | `noop`      | Nothing to do.                                                      |

`warn` counts as enforcing: it denies the current call and hands back an
acknowledgement link, so widening it would interrupt users who were not being
interrupted before.

Composition per category is `include = legacy AND base`, `exempt = legacy OR
base`, where `base` is the policy's existing specified scope for that category
if it has one, otherwise the recommendation from
`internal/risk/recommendedscopes`. Categories whose recommendation is not
`Applicable` (session-scoped detectors such as `account_identity`) are skipped:
message scoping never applied to them.

Preserving costs those categories their link to the recommendation registry:
they now carry an explicit scope, so a later registry retune no longer reaches
them. That is why it is done only for enforcing policies.

An enforcing policy whose categories cannot be resolved aborts the run
(`ErrNoCategories`) rather than being folded, because folding it would drop its
narrowing and silently widen enforcement.

## Prerequisite: legacy scope writers must be quiesced

Both scan paths honour per-category detection scopes unconditionally, so the
folded scopes take effect the moment they are written. What the fold cannot
survive is a writer that keeps filling the legacy columns underneath it.

`CreateRiskPolicy` and `UpdateRiskPolicy` still accept and persist
`message_types`, `scope_include`, and `scope_exempt`, and Platform MCP writes
them too. A policy created or edited through either surface after the fold
lands re-acquires a legacy scope, which intersects with the detection scope the
fold just wrote: the policy silently narrows to the conjunction of the two, and
`-validate` starts failing again.

Before applying in an environment, confirm no writer can still set those
columns there: the API fields rejected or ignored, and any operator scripts or
Platform MCP callers updated. Apply, then `-validate`; a nonzero `remaining` on
a later validate means a writer is still live, not that the fold missed rows.

## Prerequisite: pick the target deliberately

`-environment` is matched against a known set (`local`, `dev`, `staging`,
`prod`/`production`) rather than taken literally, so the production
confirmation cannot be dodged by spelling production the way
`GRAM_ENVIRONMENT` does. Applying also refuses when `$GRAM_DATABASE_URL` names
a production host while `-environment` claims something lesser. That check
reads the host only, so it does not catch a tunnel or port-forward to
production from localhost. A forwarded production connection still needs
`-environment=prod` and its confirmations.

## Safety properties

- Every emitted expression is compiled against the real `celenv` engine before
  it is written. A scope the engine rejects would fail the policy closed at scan
  time, so a compile failure aborts the run.
- `version` bumps only on a `cleared` fold. Findings carry
  `risk_results.risk_policy_version`; a preserved fold scans identically, so its
  existing findings must stay addressable.
- Batches are keyset-paginated and commit individually with a lock timeout and
  `FOR UPDATE SKIP LOCKED`, so a run never blocks writers for long and an
  interrupted run resumes by rerunning.
- `SKIP LOCKED` passes over rows another session holds. Apply therefore
  re-walks the candidate set (3 attempts, 2s apart) and **exits nonzero** if any
  candidate survives the last one, rather than reporting a half-folded table as
  done. `attempts` and `remaining` in the summary say what happened; rerun once
  the competing transaction is gone.
- Members of `analyzer_config` the tool does not model are copied through
  untouched, so an option written by a newer server is not destroyed by a bulk
  rewrite. A config that does not decode as a JSON object aborts the run.
- Whitespace-only legacy columns are treated as absent: they narrow nothing,
  and composing them would emit CEL the engine rejects. They are cleared, not
  folded.
- Apply is idempotent: a folded row no longer matches the candidate predicate.

## Running it

`$GRAM_DATABASE_URL` must be set. Dry run is the default and writes nothing.

```bash
# 1. Population check and dry run. Prints per-policy dispositions and a summary.
go run ./server/cmd/tools/migrations legacy-policy-scope -environment=dev

# 2. Apply.
go run ./server/cmd/tools/migrations legacy-policy-scope \
  -environment=dev -apply -confirm-environment=dev

# 3. Prove the population is empty.
go run ./server/cmd/tools/migrations legacy-policy-scope -environment=dev -validate
```

Production writes need the extra confirmation flag:

```bash
go run ./server/cmd/tools/migrations legacy-policy-scope \
  -environment=prod -apply \
  -confirm-environment=prod -confirm-production=production
```

Flags: `-batch-size` (default 100, at most 2147483647), `-lock-timeout`
(default 2s), `-statement-timeout` (default 30s). Both timeouts must be at
least 1ms and at most 2147483647ms: they are sent to PostgreSQL as whole
milliseconds, a 0ms setting means no timeout at all, and anything larger than
that ceiling the server rejects. They bound the counting queries too, so a dry run or
`-validate` fails fast instead of blocking when the table is locked.

## Output

A JSON summary on stdout, plus one structured log line per policy recording its
id, action, disposition, and the detection scopes it received. Read the dry-run
log before applying: it is the only preview of what each enforcing policy ends
up with.

```json
{
  "mode": "apply",
  "environment": "dev",
  "result": "ok",
  "elapsed_ms": 412,
  "summary": {
    "mode": "apply",
    "scanned": 37,
    "preserved": 6,
    "cleared": 31,
    "noop": 0,
    "updated": 37,
    "batches": 1,
    "attempts": 1,
    "by_action": { "block": 6, "flag": 31 },
    "remaining": 0
  }
}
```

## Recovery

Apply rewrites `analyzer_config` and `version` as well as clearing the legacy
columns, all in place, and this tool retains none of the old values. A backup
that captures only the legacy columns cannot restore the row, so take the full
set before applying:

```sql
CREATE TABLE risk_policies_legacy_scope_backup AS
SELECT id, message_types, scope_include, scope_exempt, analyzer_config, version, updated_at
FROM risk_policies
WHERE deleted IS FALSE
  AND ((message_types IS NOT NULL AND cardinality(message_types) > 0)
       OR coalesce(scope_include, '') <> ''
       OR coalesce(scope_exempt, '') <> '');
```

Restoring from it puts every folded row back exactly as it was:

```sql
UPDATE risk_policies AS p
SET message_types = b.message_types,
    scope_include = b.scope_include,
    scope_exempt = b.scope_exempt,
    analyzer_config = b.analyzer_config,
    version = b.version,
    updated_at = clock_timestamp()
FROM risk_policies_legacy_scope_backup AS b
WHERE p.id = b.id;
```

Rolling back `version` re-points findings at the version they were recorded
under, so a rollback after a `cleared` fold leaves those findings addressable
again. Drop the backup table once the fold is validated in the environment: it
holds policy scope expressions and should not outlive the migration.

## Follow-up

Dropping `message_types`, `scope_include`, and `scope_exempt` is a separate
contract migration, gated on this fold being applied and validated in every
environment. It also needs the API contract retired first: the fields are on the
create and update payloads and the `RiskPolicy` result in `design/risk`, so they
are in the published SDK, and Platform MCP both reads and writes them.
