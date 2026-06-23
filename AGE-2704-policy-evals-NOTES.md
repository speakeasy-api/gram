# AGE-2704 Policy Evals — implementation notes

Branch: `vishal/age-2704-policy-evals`. Scope: non-enforcing policy "session
replay" — run a policy over a sampled set of historical messages to gauge
efficacy/cost before enabling it.

> **Verification status:** authored without the Go/mise/atlas/sqlc/goa toolchain
> available in the authoring environment. **Nothing here has been built, codegen'd,
> or tested.** The hand-authored source-of-truth layers are complete; the
> generated layers and the Temporal/service wiring still need the steps below.
> Treat every "NOTE" as a required follow-up.

## What's implemented (committed)

| Area | Files | Status |
| --- | --- | --- |
| Schema | `server/database/schema.sql` (`policy_eval_runs`, `policy_eval_findings`) | source edited; **migration not generated** |
| Judge cost capture | `server/internal/riskjudge/judge.go`, `.../risk_analysis/llm_judge.go` | complete |
| Eval writer + stats | `.../risk_analysis/policy_eval.go` | complete |
| Judge observer wiring | `.../risk_analysis/analyze_batch.go`, `prompt_judge_batch.go` | complete |
| sqlc queries | `server/internal/risk/queries.sql` | source written; **repo not regenerated** |
| Goa design | `server/design/risk/design.go` | source written; **gen not regenerated** |
| Frontend shell + Evals tab | `client/dashboard/src/pages/security/PolicyDetail.tsx`, `.../policy-evals/*`, `routes.tsx`, `PolicyCenter.tsx` | scaffold w/ mock data |

## Required codegen + build sequence

Run from repo root with the real toolchain (`./zero --agent` first if fresh):

1. **Migration (its own PR, per CLAUDE.md):**
   `mise db:diff add-policy-eval-tables` → commit the generated
   `server/migrations/*.sql` + `atlas.sum` only. Never hand-edit them.
2. **sqlc:** regenerate `server/internal/risk/repo` from `queries.sql`
   (the repo's sqlc gen task — see `mise tasks`). This produces
   `InsertPolicyEvalFindingsParams`, `CreatePolicyEvalRunParams`,
   `ListPolicyEvalRunsRow`, `ListPolicyEvalFindingsRow`, etc. that the eval
   writer and (pending) impl reference.
3. **Goa:** regenerate `server/gen` from `design.go` (goa gen task). Produces the
   `risk` service interface methods `CreatePolicyEvalRun`, `ListPolicyEvalRuns`,
   `GetPolicyEvalRun`, `ListPolicyEvalFindings`, `CancelPolicyEvalRun` and the
   payload/result types + react-query hooks.
4. `mise build:server`, `mise lint:server`, `hk fix`, `pnpm -F dashboard type-check`.

## Remaining integration work (NOT written here — write against generated code)

### A. Service impl — `server/internal/risk/impl.go`
Implement the five generated methods. For each, gate on **`authz.ScopeOrgAdmin`**
(same as every sibling risk endpoint incl. `testDetectionRule` — do **not** use
the runtime `risk_policy:evaluate` scope), and scope every repo call to
project+org.
- `CreatePolicyEvalRun`: validate input (exactly one of `policy_id`/`candidate`;
  for `candidate`, compile its scope CEL + require a prompt for prompt_based).
  Insert a `pending` run via `CreatePolicyEvalRun`, then start the Temporal
  workflow (below) with the run id. Return the run via an `mv` model view.
- `ListPolicyEvalRuns`/`GetPolicyEvalRun`/`ListPolicyEvalFindings`: read +
  map to result types. Decode the cursor as `(created_at,id)`. Consider
  redacting `match` in the findings list unless explicitly requested.
- `CancelPolicyEvalRun`: `CancelPolicyEvalRun` query + signal/cancel the workflow.
- Add `mv` model views for `PolicyEvalRun` and `PolicyEvalFinding` in
  `server/internal/mv`.

### B. Temporal workflow + activities — `server/internal/background/`
New `PolicyEvalRunWorkflow` (per run), modeled on the fan-out shape of
`RiskAnalysisCoordinatorWorkflow` but **not** reusing it (its mark-analyzed +
outbox + DELETE-by-policy semantics are exactly what eval must avoid):
1. `SelectEvalSample` activity: resolve `sample_definition` into a pinned
   `[]chat_message_id` (auto: window/last-N stratified by role, capped at
   `max_messages`; manual: the explicit list). Persist the pinned list back
   onto the run.
2. `StartPolicyEvalRun` (running).
3. Fan out an **eval scan** over the sample in batches. Reuse the existing
   scanners by calling `AnalyzeBatch` with `JudgeObserve` set to a shared
   `risk_analysis.PolicyEvalUsageAccumulator.Observe`, **but redirect the write**
   to `risk_analysis.WriteEvalFindings` (build rows with
   `BuildPolicyEvalFindingRows(runID, projectID, orgID, msgID, findings)`) and
   **never call `MarkMessagesAnalyzed`**. Simplest path: add an eval entrypoint
   on `*AnalyzeBatch` that runs `scanPromptPolicy`/`scanStandardPolicyBatch` and
   returns `[][]Finding` without writing, so the eval activity owns the write —
   keeping all enforcement side effects out.
4. Roll up `acc.Stats()` + counts → `CompletePolicyEvalRun`. On error,
   `FailPolicyEvalRun`. Honor cancellation (check run status / workflow cancel).
5. Worker registration in `server/internal/background/worker.go`: register the
   workflow + activities. v1 may run on the existing worker; a dedicated
   `policy-eval` task queue is deferred until starvation is observed.

### C. GC schedule
Register a Temporal schedule (model on `OutboxGCWorkflow` / `AddOutboxGCSchedule`
in `worker.go`) that calls `DeleteExpiredPolicyEvalRuns` in id-batches. Set
`expires_at` on run creation (e.g. now + 30d). Required: eval findings carry raw
`match` (secret/PII).

### D. Frontend
Replace the mock data + `// TODO(AGE-2704)` call sites in
`client/dashboard/src/pages/security/policy-evals/*` with the generated hooks
from `@gram/client` (`useRiskListPolicyEvalRuns`, `useRiskGetPolicyEvalRun`,
`useRiskListPolicyEvalFindings`, `useRiskCreatePolicyEvalRun`,
`useRiskCancelPolicyEvalRun`), and the local `types.ts` with generated models.
Wire "Enable policy" to the existing `useRiskPoliciesUpdateMutation`.

## Design decisions (carried from the converged proposal)
- Separate `policy_eval_runs` + `policy_eval_findings` (NOT reusing `risk_results`:
  its `risk_policy_id`/`risk_policy_version` are NOT NULL+FK so drafts can't be
  stored, and `writeResults` fires the outbox + DELETEs live findings).
- Cost is captured for **every** judge call (not just matches) via the `Observe`
  seam, so projected go-forward cost isn't undercounted.
- Authz `org:admin`; one-shot replay (no shadow mode); auto-sampling first
  (manual picker + A/B compare deferred); labeling/precision deferred.

## Test plan
- Unit: `BuildPolicyEvalFindingRows` (grouping, dead-letter exclusion, empty),
  `PolicyEvalUsageAccumulator` (nil cost, p50/p95), judge `Observe` invoked on
  match/no-match/error.
- Integration (testenv): create run → sample resolves → findings land in
  `policy_eval_findings` and **zero** rows in `risk_results`,
  `chat_messages.risk_analyzed_at` unchanged, no outbox `RiskFindingCreatedV1`
  appended; stats rolled up; GC deletes expired runs + cascades findings.
- Authz: non-org-admin denied on all five endpoints; cross-project run/finding
  reads denied.

---
_Housekeeping: the authoring sandbox left stale `.git/*.lock.stale.*` files from
an NFS quirk; safe to `rm -f .git/*.stale.*`._
