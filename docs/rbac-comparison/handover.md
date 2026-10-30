# Handover: choosing between RBAC options B and D

This note is for an agent picking up the decision between two candidate designs for refactoring Speakeasy's authorization (RBAC). It summarises what has been built and learned so far. It doesn't recommend an option: that decision is yours to analyse and Tiago's to make. Everything below can be checked against the linked code.

## 1. Context and goal

The "Practical RBAC" RFC and its technical implementation plan (both in Notion) describe the target model. Two architecture options were built side by side from base commit `985506c4b` in `speakeasy-api/gram`, using the same scenarios, so they can be compared directly:

- **Option B, scope catalogue.** A registry of `Definition`s, one per stored scope (`mcp:connect`, `project:write`, …). It validates each request (permission known, target concrete, same tenant, ancestors present), hands the check to the **existing engine**, and labels the result (for example `legacy_broad_grant`). Code: `server/internal/authz/catalog/`.
- **Option D, declarative relationship schema.** Types, relations, blocks and permissions declared with functional builders (`s.NewType`, `s.Grant`, `s.Permission`, …), compiled at startup, and evaluated by **its own interpreter** (`relschema`) over the same rows. The engine only loads grants and writes the challenge log. Code: `server/internal/authz/declarative/`.

Neither option changes storage. Both read today's `principal_grants` rows through today's loading (`PrepareContext`).

The aim is to adopt the model incrementally: one operation at a time, activated per organization where the option supports it. It is not a big-bang rewrite of the engine in production.

## 2. Where everything is

| What | Link |
| --- | --- |
| Option B branch | [`claude/project-thread-p7id3b`](https://github.com/speakeasy-api/gram/tree/claude/project-thread-p7id3b/server/internal/authz/catalog), head `c7b47a9a3` |
| Option B guide (steps 1–2) | [`catalog/GUIDE.md`](https://github.com/speakeasy-api/gram/blob/c7b47a9a37978e0f39bd757808cfee9470c4c560/server/internal/authz/catalog/GUIDE.md) |
| Option B scenario tests | `server/internal/authz/catalog/scenarios_test.go`, `client_test.go` |
| Option B parked project step | commit [`1d6d00132`](https://github.com/speakeasy-api/gram/commit/1d6d00132741189e9050255c80c73f246e797fa0), reverted by `c7b47a9a3` |
| Option D branch | [`claude/project-thread-w79i14`](https://github.com/speakeasy-api/gram/tree/claude/project-thread-w79i14/server/internal/authz/declarative), head `0bd5c2953` |
| Option D guide (steps 1–2) | [`declarative/GUIDE.md`](https://github.com/speakeasy-api/gram/blob/0bd5c29530b059f0d433b3f9288ff5fd0113fcea/server/internal/authz/declarative/GUIDE.md) |
| Option D scenario tests | `server/internal/authz/declarative/guide_scenarios_test.go` (`TestGuideScenarios`) |
| Option D parked project step | `/mnt/project-files/rbac-option-d/step3-project-parked.patch`, applies on `0bd5c2953` |
| Slice 1 request walkthroughs | `catalog/walkthroughs/01-mcp-server-configure-vs-use.md`, `declarative/walkthroughs/01-mcp-server-configure-vs-use.md` on each branch |
| Slice 4 plans (agent history after transfer, paused at Stage A) | `/mnt/project-files/rbac-eval/option-b/slice4-stage-a.md`, `/mnt/project-files/rbac-option-d/slice4-stage-a.md` |
| Full side-by-side comparison (11 scenarios) | [`docs/rbac-comparison/b-vs-d.md`](https://github.com/speakeasy-api/gram/blob/claude/project-thread-16u1zt/docs/rbac-comparison/b-vs-d.md) |
| Summary artifact (concern table) | https://claude.ai/artifact/CdXMdE3Sm2xQbnpM6pRXB3 (private to Tiago unless shared) |

## 3. Decisions already made

These were settled by Tiago or agreed for both options. Don't reopen them without a reason.

1. **MCP rules for the new model.** Configuring a server (`mcp:write`) or reading it (`mcp:read`) no longer implies using it (`mcp:connect`). A matching MCP block always wins, including over a direct user grant. B applies this engine-wide on its branch; D applies it where the schema is switched on.
2. **Legacy wildcard connect grants are honoured, but labelled.** Project-wide and organization-wide `mcp:connect` rows still allow, marked as a legacy broad grant until they are migrated. The target is that a connect grant names one server.
3. **Each option lives in its own package**, with an overview doc and a committed walkthrough, so it can be read in an editor.
4. **Tests stay quick.** Focused unit and scenario tests only. Suites that weren't run are listed as unverified (section 8).
5. **Format.** Slice-by-slice delivery was paused in favour of a step-by-step guide. Step 1 was scaffolding, step 2 the five MCP scenarios. Step 3 (projects) was scratched from the guides and parked. Its scenarios now live only in the comparison doc.

## 4. Key differences

| Concern | B | D |
| --- | --- | --- |
| Who decides | Existing engine matches; catalogue validates before and labels after | Own evaluator decodes rows and evaluates expressions; engine loads and logs |
| How a rule is written | Flat fields: `AllowedAnchors`, `ImpliedBy`, `ExcludedBy`, `BlockWins` | Expressions: `read = (viewer ∪ editor ∪ root) − read_blocked` |
| Where rules live | Twice: in the catalogue and in the engine's tables, kept equal by `TestDefinitionsMatchEngine` | Once, in the schema. `Compile` also fails on any registered scope the schema neither reads nor excludes |
| Rollout | Engine-wide for MCP on the branch; no per-org switch | Per org via `MCPServerSchemaActive`; falls back to today's check. The callback has no implementation yet, and one switch covers MCP and projects together |
| Call site | `client.Require(ctx, catalog.MCPServerUse(target, vis)...)` | `declarative.RequireMCPServer(ctx, engine, MCPServerConnect, ref)` |
| Rows anchored outside any allowed level | Still match, only labelled | Rejected at decode, so the check fails |
| Project block vs direct user grant (R8) | Kept: `BlockWins: false`, direct grant wins (today's rule) | Block always wins; keeping R8 needs a new precedence concept |
| "A read block also blocks write" | Only in the engine's table; `ExcludedBy` names one block | Expressed directly: `write` excludes both blocks |
| Tool restrictions | Engine interceptors, outside the catalogue (`MCPToolCall` sketched) | Evaluator handles tool facts and is tested; call path (`RequireMCPTool`) sketched |
| Relationship rules (agent owner reads history) | Sketched: a closed Go enum of hand-written `Relations` plus a backend lookup | Sketched: attributes, `Is`, `Via`, `SoleAttribution` added to the schema language |
| Decision output | `Decision{Outcome, Reason, Basis}` with named reasons | Allowed / denied / undetermined plus a step-by-step trace |
| Adding a resource | Definitions plus a builder file; generic call shape already | A new type; entry points are per type until a generic `Require` arrives |
| `$represented_user` binding | Handled by the engine as today | `PolicySet` doesn't carry it (impact unverified) |
| Cost per check | Engine match, as today | Every row in a policy is decoded per check. With one schema for all types, a malformed row of any type fails unrelated checks |

## 5. Where they agree

- Same data model and grant loading.
- "Manage implies view" and "manage doesn't imply connect" give the same outcomes.
- Legacy broad connect grants are allowed and labelled in both.
- MCP blocks always win in both.
- Agents run under three policies (credential ceiling ∩ agent's grants ∩ owner's live grants), and every one must allow. Both inherit this from today's admission.
- A cross-tenant target is refused before any grant applies (B in `prepare`, D in `Evaluate`).

## 6. Behaviour changes from production

| Rule | Today | B | D (where switched on) |
| --- | --- | --- | --- |
| `mcp:write` implies `mcp:connect` | yes | no | no |
| Direct grant beats a role's block, MCP | yes | no | no |
| Direct grant beats a role's block, projects | yes | yes | no |
| Broad `mcp:connect` rows allow | yes | yes, labelled | yes, labelled |
| Unannotated tool passes a read-only grant | yes | yes | yes |

Mirrors of the old rules still exist outside the engine (the SQL list queries, the dashboard's `useRBAC`, explain). In B they're reported as remaining work, not migrated.

## 7. Shared gaps

- **Unannotated tools under a read-only grant are allowed** in both, as today. This is a product decision as well as code.
- **Tool calls aren't decided by the new model** in either option. The live path is still the legacy interceptor.
- **Listing ("which can I see?")** isn't handled by either. B has no filter operation; D doesn't cover it.
- **Time-bound access is unsupported.** `principal_grants` has no expiry. The shared sketch is a nullable `expires_at` filtered at load. Either option could then report it, as a basis in B or in the trace in D.
- **Coverage.** Only a few call sites go through either option (MCP proxy, some MCP server endpoints, and projects on parked work). Around 200 project call sites by B's count, or 80 `project:read/write` checks by D's, still call the engine directly.

## 8. Unverified

- The three-policy agent rule isn't asserted in either option's scenario tests. It follows from code that exists, but it is untested here.
- D's handling of rows with a `$represented_user` selector.
- Full server suites, integration tests, `mise` lint and the brand lint were not run in the cloud sessions (no `mise`, Docker stopped). Only focused package tests ran.
- Performance of D's decode-per-check on large policies. Not measured.
- The parked project work for both options is not on either branch head.

## 9. Open questions needing a decision

1. Should project-wide `mcp:connect` be a first-class grant, or stay legacy? Both RFC documents say connect names one server. It's a one-line change in either option.
2. Projects and R8: keep "a direct grant beats a role's block" for projects (B as built), or make blocks always win (D as built)?
3. Unannotated tools under a read-only grant: allow (today) or deny?
4. Activation: per organization (D's model), engine-wide (B as built), or per operation? Where should the switch live (feature flag or organization column), and is it per type?
5. Slice 4 (agent history after transfer): are the pre-activation rules today's rule, including PR #8110's current-owner read? Should a soft-deleted agent's last owner keep access, as the RFC says?
6. Time-bound access: is it in scope, and does it need to report "expires at" in the UI and decisions?
7. Listing: migrate filters into the new model, or keep SQL with an agreement test?

## 10. Criteria that matter for the decision

These are listed, not ranked or scored.

- **Expressiveness for the rules still to come.** That means agent ownership, teams, reviewer protection, protected sessions and conditional grants. Count how much of each fits the existing rule language without new Go code, and what has to be added.
- **One source of truth vs drift.** B's rules exist twice and rely on an agreement test. D's exist once but add an interpreter to own.
- **Safety of rollout.** Per-organization fallback, reversibility, and how well a decision explains itself when something is denied unexpectedly.
- **Migration effort.** The cost per call site, how many call sites there are, and whether the mirrors (SQL lists, dashboard) can stay consistent during the transition.
- **Failure modes.** Fail-closed behaviour, the blast radius of a malformed row (D's decode), and tenant isolation.
- **Performance** at today's grant volumes.
- **Team cost.** How easy it is to read, review and extend each option, and how much new concept it introduces.
- **Fit with the RFC and plan** (Notion: "RFC: Practical RBAC" and its implementation plan).
