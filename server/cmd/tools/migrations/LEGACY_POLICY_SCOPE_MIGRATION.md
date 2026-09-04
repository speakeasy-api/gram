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

## Prerequisite: risk-recommended-scopes must be on

Both scan paths ignore per-category detection scopes entirely while the
`risk-recommended-scopes` feature flag is off, which is still the rollout
default: `CategoryScope.InScope` returns true before consulting them, and
`CategoryScopes.Masks` returns an empty category mask. Only the legacy
policy-level scope is honoured in that state.

Applying the fold to a project whose flag is off therefore does the opposite of
what the preserve path intends: it moves an enforcing policy's narrowing into
scopes nothing reads, and the policy widens to every message surface. Apply
refuses to run without `-confirm-recommended-scopes-enabled` for this reason.

Roll the flag out first, or land a scanner change that honours policy-specified
detection scopes independently of the flag (the flag gates the recommendation
registry, not a user's explicit scope).

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
- Apply is idempotent: a folded row no longer matches the candidate predicate.

## Running it

`$GRAM_DATABASE_URL` must be set. Dry run is the default and writes nothing.

```bash
# 1. Population check and dry run. Prints per-policy dispositions and a summary.
go run ./server/cmd/tools/migrations legacy-policy-scope -environment=dev

# 2. Apply.
go run ./server/cmd/tools/migrations legacy-policy-scope \
  -environment=dev -apply -confirm-environment=dev \
  -confirm-recommended-scopes-enabled

# 3. Prove the population is empty.
go run ./server/cmd/tools/migrations legacy-policy-scope -environment=dev -validate
```

Production writes need the extra confirmation flag:

```bash
go run ./server/cmd/tools/migrations legacy-policy-scope \
  -environment=production -apply \
  -confirm-environment=production -confirm-production=production \
  -confirm-recommended-scopes-enabled
```

Flags: `-batch-size` (default 100), `-lock-timeout` (default 2s),
`-statement-timeout` (default 30s).

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
    "by_action": { "block": 6, "flag": 31 },
    "remaining": 0
  }
}
```

## Recovery

The legacy columns are cleared, not dropped, but they are cleared in place and
this tool does not retain the old values. Capture them before applying if you
want a data-level rollback:

```sql
CREATE TABLE risk_policies_legacy_scope_backup AS
SELECT id, message_types, scope_include, scope_exempt, version
FROM risk_policies
WHERE deleted IS FALSE
  AND ((message_types IS NOT NULL AND cardinality(message_types) > 0)
       OR coalesce(scope_include, '') <> ''
       OR coalesce(scope_exempt, '') <> '');
```

## Follow-up

Dropping `message_types`, `scope_include`, and `scope_exempt` is a separate
contract migration, gated on this fold being applied and validated in every
environment. It also needs the API contract retired first: the fields are on the
create and update payloads and the `RiskPolicy` result in `design/risk`, so they
are in the published SDK, and Platform MCP both reads and writes them.
