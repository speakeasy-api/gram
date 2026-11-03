# RBAC foundation migration inventory

This is the F1 deliverable (AIS-831) of the Backend Revamp milestone (parent AIS-830). It records who consumes authorization today, which ticket migrates each consumer, and the old behaviour every migration must preserve. It changes no authorization behaviour.

Two tests keep it honest, so this page cannot drift from the code:

- `TestFoundationInventory` scans the server tree and compares every authorization consumer with [`consumers.csv`](../server/internal/authz/testdata/foundation_compatibility/consumers.csv). Each row names its owning ticket.
- `TestFoundationCompatibilityCorpus` runs the hand-written decision fixtures in [`testdata/foundation_compatibility/*.json`](../server/internal/authz/testdata/foundation_compatibility/) against the unchanged `Engine` and its engine-free helpers.

Both live in `server/internal/authz/` and run with `mise run test:server ./internal/authz/`.

## Baseline

The inventory and fixtures are pinned to `main` at the commit this document landed in. The design source's own inspection baseline was `985506c4`; the package paths it names still exist, and the inventory was regenerated against the newer tree rather than copied from it.

AIS-785 (owner-scoped `agent:write`, PR #7590) is still open and is not part of this baseline. When it merges, its rebase fails `TestFoundationInventory` and `TestScopeTablesGolden` until it refreshes `consumers.csv` and the scope table golden file and adds `agent:write` owner fixtures. That is the reconciliation point AIS-831 asks for, and AG1 must not start until it has happened.

## Consumer inventory

### What counts as a consumer

The scanner records non-test Go files under `server/` that import `authz`, excluding `gen`, `internal/authz`, `internal/authztest`, `*/testdata` and `node_modules`, and classifies each use. Check literals count whether their type is written out or elided inside a `[]authz.Check` or `map[K]authz.Check` literal:

| Category     | What it records                                                                                                                                                                                                                                                                                                                                                                                |
| ------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `check`      | An `authz.Check{}` literal or a check builder (`MCPCheck`, `MCPToolCallCheck`, `MCPAuthClientCheck`, `AssistantCheck`, `ChatReadCheck`, `ChatWriteCheck`, `InferenceExecuteCheck`, `HooksIngestCheck`, `RiskPolicy*`). `via` names the deciding call when it wraps the literal (`Require`, `RequireAny`, `Evaluate`, `GrantsAuthorize`, ...); empty means the check is built for a later call. |
| `engine`     | An `Engine` method with a distinctive name: `PrepareContext`, `RefreshContext`, `CaptureAdmissionBoundary`, `PrepareMandatory`, `AuthorizeMandatory`, `DecideServerAdmission`, `ShouldEnforce`, `GetScopeOverrides`, `EvaluateLoadedGrants`, `RequireAnyUnblocked`, `FindMatched`, `InvalidateRoleCache`, `InvalidateAllRoleCaches`.                                                           |
| `evaluator`  | A helper that decides grants without the `Engine`: `GrantsAuthorize`, `GrantsSatisfy`, `GrantsContainSelector(For)`, `GrantAllows`, `ExplainGrantCheck`, `DirectOverrideGrants`, `IsDirectGrant`, `ExclusionYieldsToDirectGrants`, `IsBlocklistScope`, `RequirePluginWrite`, `APIKeyReachesMCP`.                                                                                               |
| `gate`       | An independent gate outside ordinary evaluation: sensitive content and upstream selector writes.                                                                                                                                                                                                                                                                                               |
| `projection` | Scope metadata: implications, exclusions, visibility, sub-scopes, grant surfaces, selector and key-policy validation, demo and override grant sets.                                                                                                                                                                                                                                            |
| `loader`     | Principal resolution, grant loading, `SelectorFromRow` and context plumbing.                                                                                                                                                                                                                                                                                                                   |
| `writer`     | Stored-grant writes: seeding, patches, audiences, backfills, takeovers.                                                                                                                                                                                                                                                                                                                        |
| `wiring`     | Construction and configuration.                                                                                                                                                                                                                                                                                                                                                                |

Value constructors (`NewGrant`, `NewSelector`, selector helpers) are not inventoried. A call to an exported `authz` function the scanner has not classified fails the test, so a new entry point gets a category before it spreads.

Rows are keyed by file, function, category, entry and scope, not by line, so editing a file does not churn the inventory. Adding, moving or removing a consumer does. Refresh with:

```sh
cd server && go test ./internal/authz -run TestFoundationInventory -update-foundation-inventory
```

### How owners are assigned

`foundation_owners_test.go` assigns owners in this order:

1. Scope metadata projections and stored-grant seeding belong to X1 wherever they are called from, except runtime-policy and key-policy projections, which belong to X2. Construction belongs to F5 (X4 for the demo seed).
2. Function, then file, overrides split packages that more than one ticket migrates.
3. Otherwise the package's owner is the ticket whose code entry points name it.

A consumer with no owner fails the test.

### Consumers by ticket

Counts are call sites at the baseline. Prerequisites are the Linear blocked-by relations.

| Ticket      | Prerequisites       | Sites | Categories                                                    | Packages                                                                                                                                  |
| ----------- | ------------------- | ----- | ------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| A1 AIS-879  | P1, AS1             | 8     | check 3, engine 3, evaluator 1, loader 1                      | auth, auth/assistanttokens, auth/principalcredential                                                                                      |
| AG1 AIS-856 | P1                  | 9     | check 1, engine 3, evaluator 3, loader 2                      | agentmanagement (authorize)                                                                                                               |
| AG2 AIS-857 | AG1, M5             | 32    | check 6, evaluator 3, gate 4, loader 13, writer 6             | agentmanagement (policy, delegable, connection blocks), keys                                                                              |
| AS1 AIS-860 | P1                  | 24    | check 17, engine 1, evaluator 1, loader 5                     | assistants, assistantidentity                                                                                                             |
| C1 AIS-869  | P1, AG1, AS1        | 4     | check 3, gate 1                                               | chat (point reads, writes, turn stream)                                                                                                   |
| C3 AIS-872  | Q1                  | 7     | check 4, engine 2, gate 1                                     | chat (visibility scope, conversation access, summaries, trends)                                                                           |
| E1 AIS-852  | P1                  | 8     | check 8                                                       | environments                                                                                                                              |
| F5 AIS-835  | F4                  | 16    | engine 1, wiring 15                                           | cmd/gram, cmd/directory-roles, platformmcp engine construction                                                                            |
| G1 AIS-850  | O1, P1, M5          | 76    | check 73, engine 2, evaluator 1                               | remotesessions                                                                                                                            |
| G2 AIS-849  | O1, P1, M5          | 28    | check 28                                                      | usersessions                                                                                                                              |
| I1 AIS-864  | P1, AS1             | 9     | check 5, evaluator 2, loader 2                                | mcp (workload inference), assistants (kit workload), assistantidentity (inference delegation)                                             |
| L1 AIS-867  | O1, P1              | 22    | check 22                                                      | telemetry, telemetry/analytics                                                                                                            |
| M1 AIS-859  | P1                  | 17    | check 17                                                      | toolsets                                                                                                                                  |
| M2 AIS-865  | M1                  | 30    | check 30                                                      | mcpservers, mcpendpoints, mcpmetadata, hiddenmcpservers, oktaresourceconnections, oktaserversuggestions                                   |
| M3 AIS-861  | M1, M5              | 51    | check 27, engine 13, evaluator 4, loader 7                    | mcp, metamcp                                                                                                                              |
| M4 AIS-866  | M3                  | 33    | check 32, engine 1                                            | remotemcp, tunneledmcp, unproxiedmcp                                                                                                      |
| M5 AIS-863  | M1                  | 20    | check 4, engine 2, evaluator 2, gate 1, loader 5, writer 6    | authclients, the upstream auth-client backfill, mcp mandatory decisions and auth-client resolution                                        |
| M6 AIS-862  | M2, M4              | 9     | check 3, evaluator 2, loader 4                                | plugins/roledelivery, device-agent connections and plugin servers                                                                         |
| O1 AIS-837  | F6                  | 25    | check 23, writer 2                                            | organizations, productfeatures                                                                                                            |
| O2 AIS-838  | O1, P1              | 83    | check 45, engine 2, evaluator 8, loader 20, writer 8          | access, role deletion from directory events                                                                                               |
| O3 AIS-839  | O1                  | 16    | check 15, loader 1                                            | deviceintegrations, identityapi, identityproviderconnections, slackdirectoryconnections, directory review emails                          |
| O4 AIS-844  | O1                  | 4     | check 4                                                       | externalcredentials, externalkeys, internalsessionissuers, jsonwebkeysets                                                                 |
| O5 AIS-840  | O1                  | 13    | check 12, loader 1                                            | customdomains, networkingress, assets, litellm, custom-domain health emails                                                               |
| O6 AIS-842  | O1                  | 29    | check 27, loader 2                                            | usage, spendrules, billingnotifications                                                                                                   |
| O7 AIS-841  | O1                  | 25    | check 25                                                      | aiintegrations, auditapi, businessmemory, dataexports, otel, skillefficacy                                                                |
| O8 AIS-845  | O1, P1              | 12    | check 11, engine 1                                            | agent, auth/deviceagenttoken, hooks                                                                                                       |
| P1 AIS-843  | O1                  | 9     | check 9                                                       | projects                                                                                                                                  |
| P2 AIS-851  | P1                  | 24    | check 24                                                      | deployments, packages, templates                                                                                                          |
| P3 AIS-847  | P1                  | 11    | check 11                                                      | tools, resources, variations, externalmcp, mcpregistry                                                                                    |
| P4 AIS-846  | P1                  | 15    | check 15                                                      | dashboards, widgets, sigint                                                                                                               |
| P5 AIS-848  | P1                  | 6     | check 6                                                       | triggers, assistantmemories, modelkeys                                                                                                    |
| PL1 AIS-855 | O1, P1              | 12    | check 11, writer 1                                            | plugins, plugins/assignments                                                                                                              |
| R1 AIS-868  | O1, P1              | 94    | check 56, engine 1, evaluator 1, gate 5, loader 14, writer 17 | risk and its subpackages, mcpapproval, shadowmcp/admission                                                                                |
| S1 AIS-853  | P1                  | 13    | check 11, engine 2                                            | skills                                                                                                                                    |
| S2 AIS-854  | S1, PL1             | 8     | check 4, engine 1, loader 3                                   | skills (accessible IDs), plugins (role matches, distribution, delivery principals)                                                        |
| W1 AIS-858  | O1, AG1             | 5     | check 4, evaluator 1                                          | workloadpolicy                                                                                                                            |
| X1 AIS-873  | all domain batches  | 22    | projection 15, writer 7                                       | catalogue, validation and seeding call sites across access, admin, auth, authclients, agentmanagement, platformmcp, background activities |
| X2 AIS-874  | AG2, W1, M3, X1     | 31    | evaluator 14, loader 5, projection 12                         | agents/runtimepolicy, keys policy validation                                                                                              |
| X3 AIS-875  | most domain batches | 91    | check 43, engine 10, evaluator 13, loader 24, writer 1        | platformmcp, launcher, mcp platform tool scopes                                                                                           |
| X4 AIS-876  | X1, X3              | 2     | wiring 2                                                      | demoseed                                                                                                                                  |

F1-F4, F6, C2, Q1, Z1 and Z2 own no call site: they build the foundation, characterize chat collections, or verify and retire. The total is 953 sites in 922 rows.

### Contract notes per batch

The ticket's per-consumer fields that are the same across a batch are recorded once here. Each child still records its exact operation and caller manifest before changing code, as the AIS-830 protocol requires.

**Transport and caller profiles.** Goa HTTP handlers (dashboard sessions, legacy and principal API keys, Platform MCP over OAuth) reach `Require` through `PrepareContext`. MCP runtime transports (M3, M4, M5, I1) also see agent credentials, workloads, managed assistants, embedded chat sessions and anonymous callers on public servers, and add mandatory decisions on top. Assistant-token callers (A1, AS1) are sessionless but enforced. Background activities (O3, O5, X1) act as no caller: they seed grants or resolve admin email recipients and decide nothing.

**Error mapping.** `Engine.mapError` returns `forbidden` for a denial, `unexpected` for missing prepared grants, invalid checks and empty check lists, and `ShouldEnforce` returns `unauthorized` without an auth context. Handlers frequently remap a denial to `not_found` to hide existence; those remaps are per-handler and belong in each child's manifest. Mandatory decisions return a `MandatoryDecision`, not an error, and errors there are infrastructure failures.

**Independent gates.** Sensitive-content access (C1, C3, L1, X3), upstream selector writes (AG2, M5, R1), the mandatory server and auth-client decisions (M3, M5), demo and support grant sets, scope overrides and org-less login stay outside ordinary evaluation and keep their own fixtures and tests.

**Query and projection consumers.** `Filter` and `FindMatched` (lists in M2, M3, S1, X3), the SQL projections `ListAccessibleMCPServersForUser` (M6) and `ListAccessibleSkillsForUser` (S2), chat collection queries (C2 characterizes, Q1 and C3 migrate), `explain_access` and challenge resolution (O2), and runtime-policy delegation containment (X2).

**Cutover dependency routes.** These must switch evaluators together, per the RFC: chat → agent ownership (C1, C3 with AG1); plugin publication → project and organization (PL1 with O1, P1); agent credential and workload admission → runtime policy (A1, W1 with X2); auth-client use → represented-user policy (M5 with G1, G2, AG2).

### Consumers outside `server/internal`

| Consumer                                                                                                                                   | Owner                                                                              |
| ------------------------------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------- |
| `server/cmd/gram` engine and provisioner construction                                                                                      | F5 (in the inventory)                                                              |
| `server/cmd/directory-roles` engine construction                                                                                           | F5 (in the inventory)                                                              |
| `server/design/access/design.go` scope enums, and the SDK generated from them                                                              | X1 for catalogue parity, X4 for SDK parity; no Goa or SDK scope changes by default |
| Dashboard gates: `client/dashboard/src/hooks/useRBAC.ts`, `components/require-scope.tsx`, `pages/access/types.ts` and pages that call them | X4                                                                                 |
| `docs/rbac.md` and the `gram-rbac` skill                                                                                                   | X1 and Z2                                                                          |

The CLI, functions runner and TypeScript functions framework do not evaluate grants.

### Gaps

No consumer is left without a ticket. Three groups are retained primitives rather than migrations, and their owner only has to confirm they still behave the same:

- Admin email resolution (`ResolveOrganizationAdminEmails` in billing, custom-domain health and directory review emails). It reads role membership to address mail and authorizes nothing.
- Grant writers that keep the stored schema (`PatchRoleGrantsTx`, `PatchPrincipalGrants`, audience replacement, auth-permission takeover). The foundation contract keeps storage unchanged, so these stay; only their validation moves to X1.
- The platform MCP engine constructed inside `AccessReadService.GetMCPAccess`, which F5 should replace with the injected client rather than migrate in place.

## Frozen compatibility corpus

Each fixture names its caller, grants (or independent admitted policies), the entry point that decides and the expected outcome, written by hand from today's behaviour. The runner builds contexts with the same trusted setters and admission paths production uses, and never reads a new registry. Contract coverage is itself tested: `TestFoundationCompatibilityCorpusCoversContract` fails if any contract below has no fixture.

| Contract                      | File       | What is pinned                                                                                                                                                                                                                             |
| ----------------------------- | ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `nontransitive_expansion`     | matching   | Only the scopes listed in `scopeExpansions` satisfy a check; `org:admin` does not reach `project:read`, `log:read` or `chat:read`.                                                                                                         |
| `root`                        | matching   | `root` satisfies everything except auth-client use, and blocks still apply to it.                                                                                                                                                          |
| `permissive_allow_matching`   | matching   | Grant keys a check omits are skipped for ordinary allows; strict checks require every grant constraint.                                                                                                                                    |
| `strict_block_matching`       | matching   | Blocks match only checks that carry every block constraint.                                                                                                                                                                                |
| `strict_auth_client_matching` | matching   | Auth-client grants match strictly and root never reaches them.                                                                                                                                                                             |
| `selector_validation`         | matching   | `ValidateSelector` accepted keys, kinds, `inference:execute` wildcard IDs and `$represented_user` placement.                                                                                                                               |
| `direct_exceptions`           | precedence | Only explicit concrete grants to the user outrank inherited blocks, as far as they reach; wildcard, inferred, agent and role grants do not.                                                                                                |
| `own_blocks`                  | precedence | A user's own block beats every allow, including a direct exception.                                                                                                                                                                        |
| `raw_subtraction`             | precedence | Risk-policy evaluate minus bypass ignores principal precedence and subtracts per server.                                                                                                                                                   |
| `alternatives`                | precedence | `RequireAny` falls through blocked alternatives; `Require` needs every check.                                                                                                                                                              |
| `block_veto`                  | precedence | `RequireAnyUnblocked` rejects a block on any alternative unless a direct exception covers it.                                                                                                                                              |
| `policy_intersection`         | callers    | Agent credentials, principal API keys and workloads must satisfy the check in every policy; different grants may satisfy each one; empty policies deny; unadmitted credentials error.                                                      |
| `represented_user`            | callers    | `$represented_user` binds to the signed-in human, binds nobody for embedded sessions and assistants, and adds the represented user's policy for auth-client checks only.                                                                   |
| `enforcement_profile`         | callers    | Which callers `Require` enforces: legacy keys and sessionless internal paths skip (but never use auth clients); Platform MCP humans and assistants are enforced; org-less, demo, support and override sessions get their fixed grant sets. |
| `input_errors`                | callers    | Empty or wildcard resource IDs, empty check lists and malformed auth-client checks are errors, not denials.                                                                                                                                |
| `lists`                       | callers    | `Filter` and `FindMatched` apply blocks and keep order; unenforced filters drop auth-client checks.                                                                                                                                        |
| `mandatory_gates`             | callers    | Public servers admit anonymous callers but keep human limits; skipped enforcement is never read as allow; credential policies intersect.                                                                                                   |
| `nil_empty_writes`            | storage    | Nil selectors write one wildcard row, empty selectors write none, duplicates collapse, invalid selectors reject the write.                                                                                                                 |
| `malformed_grants`            | storage    | Stored selectors that are not string maps fail to parse.                                                                                                                                                                                   |
| `assistant_defaults`          | storage    | Assistant read and write defaults for system roles, still subject to blocks.                                                                                                                                                               |

The scope table golden file (`scope_tables.golden`) pins every scope's visibility, resource kind, grant surface, satisfying scopes, exclusion, closure, sub-scopes, accepted selector keys and role defaults. `runtimepolicy/testdata/runtime_scopes.golden` pins each scope's agent-runtime lifecycle.

### Covered by existing database-backed tests

These depend on stored rows, so the corpus points at the tests that already pin them instead of duplicating database setup:

- Loading merges user and role grants: `TestLoadGrants_loadsUserAndRoleGrants` (`authz/load_test.go`).
- Re-seeding does not restore removed system-role grants: `TestSeedSystemRoleGrantsBootstrapsGlobalRoles` (`authz/load_test.go`).
- Legacy API keys are decided on their own `api_key:` grants for mandatory checks: `TestAuthorizeMandatoryForLegacyAPIKeys`, `TestLegacyKeysNeverUseTheirCreatorsConnection` (`authz/api_key_policy_test.go`).
- Credential and workload admission reload and conjoin policies: `authz/credential_admission_test.go`, `authz/workload_admission_test.go`.

### Behaviours worth knowing before migrating

The corpus freezes these as they are. Changing any of them is a product decision with its own ticket, not part of a migration:

- A project-scoped MCP grant matches a check that omits `project_id` (`allow_project_grant_matches_check_without_project_dimension`). This is the cross-project exposure flagged in the capability inventory for the 42 dimensionless `mcp:read`/`mcp:write` checks; the M slices fix the checks, and the fixture changes only when they do.
- A disposition-scoped allow reaches a tool with no annotations for ordinary checks (`allow_disposition_grant_reaches_unannotated_tool`), though not as a direct exception past a block.
- A project-scoped block is skipped by a check without `project_id` (`block_project_block_skips_check_without_project_dimension`).
- Block implications differ by family: `environment:blocked_read` also blocks writes, but `mcp:blocked_read` does not block `mcp:write`.
- Inside an owner policy, the owner's direct exceptions apply as they would for the owner (`credential_owner_direct_exception_applies_inside_owner_policy`), while an agent's own grants never outrank a block.

## Using this in later tickets

- F6 compares shadow decisions against this corpus, not against new compiler output.
- F3's grant evaluator (`authz.RegistryEvaluator`) runs every ordinary-human `require`, `require_any` and `risk_policy_applies` fixture through compiled test declarations (`TestRegistryEvaluatorFoundationCorpus`). Fixtures it cannot express are listed with a reason in `corpusSkips`; the test fails if one drops out silently.
- Each migration child adds fixtures for its operations when the existing ones do not cover them, and moves its consumers out of `consumers.csv` only by changing the code they point at.
- A PR that changes an expectation here is an access change and needs its own approval, per the AIS-830 release gate.
