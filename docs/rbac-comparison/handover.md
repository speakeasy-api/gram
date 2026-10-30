# Handover: choosing an RBAC architecture

This note is for an agent picking up the decision between two candidate designs for refactoring Speakeasy's authorization (RBAC). It summarises what has been built and learned so far. It doesn't recommend an option: that decision is yours to analyse and Tiago's to make. Everything below can be checked against the linked code.

> **Status as of 2026-10-30 14:30Z.** An external review (section 11) gave a provisional recommendation: Schema if the relationship-heavy roadmap is committed scope, Catalog if the goal is a bounded cleanup. It set an evidence gate for Schema. Schema has since passed that gate's six acceptance cases at `93da99dc4` (section 12). **Tiago is leaning towards Schema** as the cleaner long-term architecture, but **no final decision has been made.** Neither prototype is production-ready.

## 1. Context and goal

The "Practical RBAC" RFC and its technical implementation plan (both in Notion) describe the target model. Two architecture options were built side by side from base commit `985506c4b` in `speakeasy-api/gram`, using the same scenarios, so they can be compared directly:

- **Scope Catalog ("Catalog").** A registry of `Definition`s, one per stored scope (`mcp:connect`, `project:write`, …). It validates each request (permission known, target concrete, same tenant, ancestors present), hands the check to the **existing engine**, and labels the result (for example `legacy_broad_grant`). Code: `server/internal/authz/catalog/`.
- **Relationship Schema ("Schema").** Types, relations, blocks and permissions declared with functional builders (`s.NewType`, `s.Grant`, `s.Permission`, …), compiled at startup, and evaluated by **its own interpreter** (`relschema`) over the same rows. The engine only loads grants and writes the challenge log. Code: `server/internal/authz/declarative/`.

Earlier notes, branch commits and project-file paths call these "option B" (Scope Catalog) and "option D" (Relationship Schema).

Neither option changes storage. Both read today's `principal_grants` rows through today's loading (`PrepareContext`).

The aim is to adopt the model incrementally: one operation at a time, activated per organization where the option supports it. It is not a big-bang rewrite of the engine in production.

## 2. Where everything is

| What | Link |
| --- | --- |
| Catalog branch | [`claude/project-thread-p7id3b`](https://github.com/speakeasy-api/gram/tree/claude/project-thread-p7id3b/server/internal/authz/catalog), head `c7b47a9a3` |
| Catalog guide (steps 1–2) | [`catalog/GUIDE.md`](https://github.com/speakeasy-api/gram/blob/c7b47a9a37978e0f39bd757808cfee9470c4c560/server/internal/authz/catalog/GUIDE.md) |
| Catalog scenario tests | `server/internal/authz/catalog/scenarios_test.go`, `client_test.go` |
| Catalog parked project step | commit [`1d6d00132`](https://github.com/speakeasy-api/gram/commit/1d6d00132741189e9050255c80c73f246e797fa0), reverted by `c7b47a9a3` |
| Schema branch | [`claude/project-thread-w79i14`](https://github.com/speakeasy-api/gram/tree/claude/project-thread-w79i14/server/internal/authz/declarative), head `93da99dc4` (evidence gate); MCP scenarios at `0bd5c2953` |
| Schema guide (steps 1–3) | [`declarative/GUIDE.md`](https://github.com/speakeasy-api/gram/blob/93da99dc4/server/internal/authz/declarative/GUIDE.md); step 3 is the evidence gate |
| Schema evidence-gate tests | `declarative/chat_access_test.go` (`TestAgentHistoryAcceptance`), `chat/agent_history_schema_test.go` (`TestAgentHistorySchema`, Postgres), `TestNewScopeNeedsADecision`, `TestMalformedRowIsolation` |
| Schema scenario tests | `server/internal/authz/declarative/guide_scenarios_test.go` (`TestGuideScenarios`) |
| Schema parked project step | `/mnt/project-files/rbac-option-d/step3-project-parked.patch`, applies on `0bd5c2953` |
| Slice 1 request walkthroughs | `catalog/walkthroughs/01-mcp-server-configure-vs-use.md`, `declarative/walkthroughs/01-mcp-server-configure-vs-use.md` on each branch |
| Slice 4 plans (agent history after transfer; Catalog paused at Stage A, Schema built as the evidence gate) | `/mnt/project-files/rbac-eval/option-b/slice4-stage-a.md`, `/mnt/project-files/rbac-option-d/slice4-stage-a.md` |
| Full side-by-side comparison (11 scenarios) | [`docs/rbac-comparison/catalog-vs-schema.md`](https://github.com/speakeasy-api/gram/blob/claude/project-thread-16u1zt/docs/rbac-comparison/catalog-vs-schema.md) |
| Summary artifact (concern table) | https://claude.ai/artifact/CdXMdE3Sm2xQbnpM6pRXB3 (private to Tiago unless shared) |

## 3. Decisions already made

These were settled by Tiago or agreed for both options. Don't reopen them without a reason.

1. **MCP rules for the new model.** Configuring a server (`mcp:write`) or reading it (`mcp:read`) no longer implies using it (`mcp:connect`). A matching MCP block always wins, including over a direct user grant. Catalog applies this engine-wide on its branch; Schema applies it where the schema is switched on.
2. **Legacy wildcard connect grants are honoured, but labelled.** Project-wide and organization-wide `mcp:connect` rows still allow, marked as a legacy broad grant until they are migrated. The target is that a connect grant names one server.
3. **Each option lives in its own package**, with an overview doc and a committed walkthrough, so it can be read in an editor.
4. **Tests stay quick.** Focused unit and scenario tests only. Suites that weren't run are listed as unverified (section 8).
5. **Format.** Slice-by-slice delivery was paused in favour of a step-by-step guide. Step 1 was scaffolding, step 2 the five MCP scenarios. Step 3 (projects) was scratched from the guides and parked. Its scenarios now live only in the comparison doc.

## 4. Key differences

| Concern | Catalog | Schema |
| --- | --- | --- |
| Who decides | Existing engine matches; catalog validates before and labels after | Own evaluator decodes rows and evaluates expressions; engine loads and logs |
| How a rule is written | Flat fields: `AllowedAnchors`, `ImpliedBy`, `ExcludedBy`, `BlockWins` | Expressions: `read = (viewer ∪ editor ∪ root) − read_blocked` |
| Where rules live | Twice: in the catalog and in the engine's tables, kept equal by `TestDefinitionsMatchEngine`. `ImpliedBy`, `ExcludedBy`, `BlockWins` describe what the engine does; `AllowedAnchors` only labels | Once, in the schema, **for migrated checks only**. Since `93da99dc4`, a newly registered scope fails startup until it's mapped or listed out of schema with a reason (before that, the wiring excluded it automatically) |
| Rollout | Engine-wide for MCP on the branch; no per-org switch | Per org via `MCPServerSchemaActive`; falls back to today's check. The callback has no implementation yet, and one switch covers MCP and projects together |
| Call site | `client.Require(ctx, catalog.MCPServerUse(target, vis)...)` | `declarative.RequireMCPServer(ctx, engine, MCPServerConnect, ref)` |
| Rows anchored outside any allowed level | Still match, only labelled | Rejected at decode, so the check fails |
| Project block vs direct user grant (R8) | Kept: `BlockWins: false`, direct grant wins (today's rule) | Block always wins; keeping R8 needs a new precedence concept |
| "A read block also blocks write" | Only in the engine's table; `ExcludedBy` names one block | Expressed directly: `write` excludes both blocks |
| Tool restrictions | Engine interceptors, outside the catalog (`MCPToolCall` sketched) | Evaluator handles tool facts and is tested; call path (`RequireMCPTool`) sketched |
| Relationship rules (agent owner reads history) | Sketched: a closed Go enum of hand-written `Relations` plus a backend lookup | **Implemented** at `93da99dc4`: `Attr`, `Is`, `Via`, `Unless`, with direct reads and listing; listing's owner branches are a hand-written SQL mirror |
| Decision output | `Decision{Outcome, Reason, Basis}` with named reasons | Allowed / denied / undetermined plus a step-by-step trace |
| Adding a resource | Definitions plus a builder file; generic call shape already | A new type; entry points are per type until a generic `Require` arrives |
| `$represented_user` binding | Handled by the engine as today | `PolicySet` doesn't carry it (impact unverified) |
| Cost per check | Engine match, as today; successful checks are evaluated again to get decision metadata | Every row in a policy is decoded per check, plus an agent row load for session reads. A malformed row in a mapped scope fails **every** schema check for that caller (500, fail closed); `AuditGrants` finds such rows before activation. Neither side is measured |

## 5. Where they agree

- Same data model and grant loading.
- "Manage implies view" and "manage doesn't imply connect" give the same outcomes.
- Legacy broad connect grants are allowed and labelled in both.
- MCP blocks always win in both.
- Agents run under three policies (credential ceiling ∩ agent's grants ∩ owner's live grants), and every one must allow. Both inherit this from today's admission.
- A cross-tenant target is refused before any grant applies (Catalog in `prepare`, Schema in `Evaluate`).

## 6. Behaviour changes from production

| Rule | Today | Catalog | Schema (where switched on) |
| --- | --- | --- | --- |
| `mcp:write` implies `mcp:connect` | yes | no | no |
| Direct grant beats a role's block, MCP | yes | no | no |
| Direct grant beats a role's block, projects | yes | yes | no |
| Broad `mcp:connect` rows allow | yes | yes, labelled | yes, labelled |
| Unannotated tool passes a read-only grant | yes | yes | yes |

Mirrors of the old rules still exist outside the engine (the SQL list queries, the dashboard's `useRBAC`, explain). In Catalog they're reported as remaining work, not migrated.

## 7. Shared gaps

- **Unannotated tools under a read-only grant are allowed** in both, as today. This is a product decision as well as code.
- **Tool calls aren't decided by the new model** in either option. The live path is still the legacy interceptor.
- **Listing ("which can I see?").** Catalog has no filter operation. Schema lists sessions at `93da99dc4`: the schema decides "all or not", and a hand-written SQL mirror applies the owner branches, held equal by an agreement test. Generating SQL from the schema isn't built. Projects and MCP servers aren't listed through either.
- **Coverage of whole operations.** Enforcement, listing, capabilities, explain and tool restrictions must all agree. In both options, tool execution still uses the legacy interceptor, and SQL lists and dashboard checks are separate mirrors.
- **Time-bound access is unsupported.** `principal_grants` has no expiry. The shared sketch is a nullable `expires_at` filtered at load. Either option could then report it, as a basis in Catalog or in the trace in Schema.
- **Coverage.** Only a few call sites go through either option (MCP proxy, some MCP server endpoints, and projects on parked work). Around 200 project call sites by Catalog's count, or 80 `project:read/write` checks by Schema's, still call the engine directly.

## 8. Unverified

- The three-policy agent rule isn't asserted in either option's scenario tests. It follows from code that exists, but it is untested here.
- Schema's handling of rows with a `$represented_user` selector.
- Full server suites, integration tests, `mise` lint and the brand lint were not run in the cloud sessions (no `mise`, Docker stopped). Only focused package tests ran.
- Performance on both sides. Schema decodes every row per check and loads an agent row per session read; Catalog re-evaluates successful checks for metadata. Neither is measured.
- Expiry behaviour for already-prepared contexts and long-lived operations (the time-bound sketch only filters at load).
- The external review was static inspection only: no tests run, and it didn't read the Notion RFC, the artifact, the parked Schema project patch or the slice 4 plans.
- The parked project work for both options is not on either branch head.

## 9. Open questions needing a decision

1. Should project-wide `mcp:connect` be a first-class grant, or stay legacy? Both RFC documents say connect names one server. It's a one-line change in either option.
2. Projects and R8: keep "a direct grant beats a role's block" for projects (Catalog as built), or make blocks always win (Schema as built)?
3. Unannotated tools under a read-only grant: allow (today) or deny?
4. Activation: per organization (Schema's model), engine-wide (Catalog as built), or per operation? Where should the switch live (feature flag or organization column), and is it per type?
5. Agent history: Schema's evidence gate chose defaults that need confirming: no capture-time marker (per-organization activation only), `chats.user_id` counts as the author though it comes from a payload email, and ownership isn't capped by a narrowed credential's second policy. Should a soft-deleted agent's last owner keep access, as the RFC says?
6. Time-bound access: is it in scope, and does it need to report "expires at" in the UI and decisions?
7. Listing: keep hand-written SQL mirrors with agreement tests (as Schema's gate does), or invest in generating list filters from the schema?
8. **The scope question the review raised:** is the committed objective the broader relationship-heavy model (favours Schema, subject to the gate) or a bounded scope/MCP cleanup (favours Catalog)?
9. Rollback: switching the new model off may restore broader legacy permissions (for example write-implies-connect). Is that acceptable?
10. Malformed rows in Schema: fail every check for the caller (as built), or narrow the blast radius per type?

## 10. Criteria that matter for the decision

These are listed, not ranked or scored.

- **Expressiveness for the rules still to come.** That means agent ownership, teams, reviewer protection, protected sessions and conditional grants. Count how much of each fits the existing rule language without new Go code, and what has to be added.
- **One source of truth vs drift.** Catalog's rules exist twice and rely on an agreement test. Schema's exist once but add an interpreter to own.
- **Safety of rollout.** Per-organization fallback, reversibility, and how well a decision explains itself when something is denied unexpectedly.
- **Migration effort.** The cost per call site, how many call sites there are, and whether the mirrors (SQL lists, dashboard) can stay consistent during the transition.
- **Failure modes.** Fail-closed behaviour, the blast radius of a malformed row (Schema's decode), and tenant isolation.
- **Performance** at today's grant volumes.
- **Team cost.** How easy it is to read, review and extend each option, and how much new concept it introduces.
- **Fit with the RFC and plan** (Notion: "RFC: Practical RBAC" and its implementation plan).

## 11. External review (2026-10-30, before the evidence gate)

An independent review read this handover, the comparison doc and both implementations (Catalog at `c7b47a9a3`, Schema at `0bd5c2953`). It was static inspection only. Its conclusions:

- **Provisional recommendation:** Schema as the target if the relationship-heavy Practical RBAC roadmap is committed scope; Catalog if the goal is a bounded cleanup of scope checks and MCP semantics. Moderate confidence. Not an approval to implement. "Revise before approval" for both prototypes.
- **The real choice:** Catalog keeps the existing evaluator and adds a validating, labelling layer. Schema introduces a replacement evaluator. Avoid an indefinite mix of authorities, and don't pick Catalog as a stepping stone to Schema, which could mean two migrations.
- **Findings:**
  1. Schema's new-scope safeguard was overstated: the wiring excluded unmapped scopes automatically. *(Fixed in Schema at `93da99dc4`.)*
  2. Schema is one policy definition only for migrated checks. Judge migration by complete operation coverage, not converted handler calls.
  3. Catalog's catalog is partly descriptive: the engine executes the policy, `AllowedAnchors` labels rather than restricts, and agreement tests reduce drift without proving equivalence.
  4. Schema needs an explicit failure-isolation contract for malformed rows. *(Tested and documented in Schema at `93da99dc4`; see section 12.)*
  5. Neither option proves expiry behaviour. Define the revocation window and check-time behaviour.
  6. Performance is unmeasured on both sides.
- **Product decisions to keep separate from the architecture:** keep MCP configure/use separation and block precedence; don't make project-wide connect first-class just because it's a small change; don't let Schema's missing R8 support decide project precedence implicitly; for tools, an unknown disposition shouldn't satisfy a read-only restriction, though an independent unrestricted grant may still allow; define rollback semantics.
- **Evidence gate for Schema:** build agent-history access after ownership transfer, including listing and direct reads, with six acceptance cases (below). Reassess Schema if it needs repeated domain-specific escape hatches or can't support listing coherently.

## 12. Schema's evidence gate results (`93da99dc4`)

Built behind the same per-organization switch as MCP servers (off in production). It's a rough proof, not production code. Details are in Schema's guide, step 3.

| # | Acceptance case | Result |
| --- | --- | --- |
| 1 | New owner gains access; former owner loses it | Pass. A `chat:read` grant restores the former owner |
| 2 | Agent management alone grants no transcript access | Pass, even with all four `agent:*` scopes and `org:admin` |
| 3 | Human attribution isn't overridden by a named agent | Pass, for member and external-user authors |
| 4 | Missing facts and cross-tenant references never allow | Pass: dangling agent is undetermined; foreign agent, latched agent, foreign session and non-person caller are denied |
| 5 | Listing and direct reads agree, including pagination | Pass, in Postgres, per caller, before and after transfer, one row per page |
| 6 | Every admitted policy is enforced | Pass for grants. Ownership isn't capped by a second policy, as today; whether it should be is a product decision |

What it showed against the review's reassessment triggers:

- **Language growth:** four additions (`Attr`, `Is`, `Via`, `Unless`) plus `Request.Subject`. No evaluator escape hatches. Two pieces of policy-adjacent Go remain: `chatObject` (owner set only for an eligible agent) and `PlanChatList`.
- **Listing:** coherent but not generated. The schema answers "all or not", and the owner branches are a hand-written SQL mirror held equal by an agreement test. A mutation check shows the test catches drift. This is the main cost the slice exposed.
- **New scopes:** an unmapped registered scope now fails startup (`TestNewScopeNeedsADecision`).
- **Malformed rows:** fail closed for the whole policy (500), including on unrelated resources and types. Out-of-schema rows are never decoded. Silently skipping rows was rejected because dropping a block widens access. `AuditGrants` lists unreadable rows for a pre-activation cleanup. Per-type isolation isn't built.
- **Still open:** writes still use today's inline owner check; one switch covers MCP servers and sessions; per-check cost unmeasured.

Catalog has no equivalent slice built. Its slice 4 plan stayed at Stage A.

