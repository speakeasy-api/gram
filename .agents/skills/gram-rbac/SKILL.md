---
name: gram-rbac
description: Use when a task touches authorization in Speakeasy — gating a handler or list endpoint, `authz.Require` / `RequireAny` / `RequireAnyUnblocked` / `Can` / `Evaluate` / `Filter`, typed refs like `authz.MCPServer(id, project).Write()` or `authz.Project(p).Environments().Create()`, `authz.Check{...}` literals, `authz.EnvironmentCheck` / `MCPCheck` / `MCPToolCallCheck`, adding or changing a scope, permission, resource type, block or dimension in `registry.Definitions` (`authz/internal/registry/definitions.go`), system role defaults, agent-safe scopes (`SafeSince`, `AgentUnsafe`), `mise run gen:authz`, `refs_gen.go`, `scopes_gen.go`, `tables_gen.go`, the `norawauthzchecks` lint, "invalid authz check", `ErrInvalidCheck`, `CodeForbidden` from authz, or RBAC state in the dashboard (`useRBAC`, `RequireScope`, access pages).
metadata:
  relevant_files:
    - "server/internal/authz/**/*.go"
    - "server/internal/authztest/**/*.go"
    - "server/internal/access/**/*.go"
    - "server/internal/access/**/*.sql"
    - "server/internal/agents/runtimepolicy/**/*.go"
    - "server/design/access/**"
    - "client/dashboard/src/pages/access/**"
    - "client/dashboard/src/hooks/useRBAC.ts"
    - "glint/no_raw_authz_checks.go"
---

# Speakeasy RBAC

The whole permission model is one Go value, `registry.Definitions` in `server/internal/authz/internal/registry/definitions.go`. It is build-time input only: `mise run gen:authz` validates it and writes the scope constants (`scopes_gen.go`), the typed request refs (`refs_gen.go`, including methods such as `ToolCall` declared by a permission's `DimensionSets`; only the project collections are hand-written in `refs.go`) and every derived table (`tables_gen.go`: expansions and blocks, selector key allowlists, the scope catalogue, system role scopes, agent eligibility) into package `authz`, which never imports the registry. Being under `authz/internal/`, the registry cannot be imported from outside `authz`. The scope enum in the Goa design and the `docs/rbac.md` scope table come from the generated values. One server-side evaluator, `authz.Engine`, is canonical. Two list queries still mirror most of its matching and precedence in SQL (`ListAccessibleMCPServersForUser`, `ListAccessibleSkillsForUser`); a change to matching or blocks must update them too, until the SQL prefilter replaces them. They are not exact mirrors: the skill list leaves out a skill whose only grant is `skill:write` blocked by `skill:blocked_write`, although the engine still allows `skill:read` on it (kept on purpose; see the query's comment). **Change the model, regenerate, and let the tests tell you what else moved.** Never hand-edit a derived table.

## Mental model

| Term             | Meaning                                                                                                                                                                                                                                                    |
| ---------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Resource type    | A `ResourceType` in the model: `Kind` (`mcp`, `environment`, `project`, ...), optional `Parent` (`project`), its `Dimensions`, and its `Permissions`.                                                                                                      |
| Permission       | One action on a type (`read`, `write`, `connect`). Its scope is `<kind>:<action>`; `Implies` lists the actions a grant of it also satisfies; `DimensionSets` adds ref methods that take some of the type's dimensions as arguments (connect's `ToolCall`). |
| Block            | A permission's `Exclusions: Blocked(...)` generate `<kind>:blocked_<action>`. A block also takes away every permission that implies it.                                                                                                                    |
| Grant            | Scope + selector held by a principal (user, role, `user:all`, agent, API key). Build with `authz.NewGrant` / `NewGrantWithSelector`.                                                                                                                       |
| Check            | What a request asks for. Build it with a typed ref, never by hand.                                                                                                                                                                                         |
| Dimension values | A concrete value matches an equal grant value or `*`. `authz.SelectorValueUnknown` (`$unknown`) never satisfies a constrained allow and always matches a constrained block. An empty value or `*` in a check is invalid.                                   |
| Precedence       | A user's direct grant naming a concrete resource outranks a block inherited from a role or `user:all`. Lives in `precedence.go`.                                                                                                                           |

## Gating a handler

```go
// one resource
if err := s.authz.Require(ctx, authz.MCPServer(id, project).Write()); err != nil {
	return err
}
// no resource yet (create), or a lookup that missed (do not leak existence)
if err := s.authz.Require(ctx, authz.Project(p).Environments().Create()); err != nil {
	return err
}
if err := s.authz.Require(ctx, authz.Project(p).Environments().Read()); err != nil {
	return err
}
// a project-wide grant OR a grant on the resource, and a block on either refuses
if err := s.authz.Require(ctx, authz.AnyOf(authz.Project(p).Write(), authz.Skill(id).Write())); err != nil {
	return err
}
// a branch, not a denial (no challenge logged)
ok, err := s.authz.Can(ctx, authz.Chat(id).Read())
// discovery: "could they act on any matching resource?"
ok, err := s.authz.Can(ctx, authz.MCPServer(id, p).Connect().Probe())
```

| Need                                             | Use                                                                                                                                                                    |
| ------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Refuse unless allowed                            | `Require(ctx, checks...)` (all must pass)                                                                                                                              |
| Several equivalent grants, blocks must still win | `Require(ctx, authz.AnyOf(...))` (same as `RequireAnyUnblocked`)                                                                                                       |
| Branch on the answer                             | `Can(ctx, ...)`. Accepts a single `AnyOf` and probes. Returns an error, not false, for an invalid check.                                                               |
| Narrow a list                                    | `Filter(ctx, checks)` returns the allowed `ResourceID`s. `FindMatched` returns a bool per check, for many checks that share one resource id (tools in a `tools/list`). |
| MCP tool call                                    | `authz.MCPServer(id, p).ToolCall(tool, disposition)`. An empty disposition becomes unknown, so disposition blocks apply to unannotated tools.                          |

Ref constructors take typed ids, never a raw string or `uuid.UUID`: `authz.Environment(authz.EnvironmentIDOf(env.ID), authz.ProjectIDOf(projectID))`. Each kind that has a ref has an id type generated in `ids_gen.go` with two constructors, `<Kind>IDOf(uuid.UUID)` and `<Kind>IDFrom(string)`, so ids of different kinds cannot be swapped. The hand-written `mcp_auth_client`, `risk_policy` and `inference` checks keep their own constructors, listed below. An id that cannot name a resource (the nil uuid, an empty string, `*`, a `$` marker, a string containing whitespace) is kept as absent, and every check built from it is refused with `ErrInvalidCheck`: an absent id never grants. In the examples and the migration table here `id`, `envID`, `p` and `project` are such typed ids, already built with `IDOf` or `IDFrom`. The argument order is always the resource, then its project. A project collection (`Project(p).Environments()`, `.MCPServers()`, `.Assistants()`) builds a check on the resource type whose resource id is the project id, exactly what `EnvironmentCheck(scope, p, p)` built before, so existing grants match it the same way and `Filter` returns the project id for it.

When migrating a package from the old constructors:

| Before                                      | After                                                               |
| ------------------------------------------- | ------------------------------------------------------------------- |
| `EnvironmentCheck(s, envID, p)`             | `authz.Environment(envID, p).Read()` / `.Write()`                   |
| `EnvironmentCheck(s, p, p)`                 | `authz.Project(p).Environments().Read()` / `.Write()` / `.Create()` |
| `MCPCheck(ScopeMCPWrite, p, p)`             | `authz.Project(p).MCPServers().Create()`                            |
| `AssistantCheck(ScopeAssistantWrite, p, p)` | `authz.Project(p).Assistants().Create()`                            |
| `MCPToolCallCheck(...)`                     | `authz.MCPServer(id, p).ToolCall(tool, disposition)`                |

Keep `Require` and its `CodeForbidden` branch where the code already has one. Swapping it for `Can` keeps enforcement the same but stops logging the challenge, so do that only where the denial is not a refusal the user sees.

Refs exist for every kind in the model except `mcp_auth_client` (`MCPAuthClientCheck`), `risk_policy` (`RiskPolicyApplies` / bypass checks) and `inference` (`InferenceExecuteCheck`). The list sits at the end of `refs_gen.go`.

For a list endpoint, load the candidate rows, build one ref per row, call `Filter`, and rebuild the response from the ids it returns. Never call `Require` per item.

In the handler's test, add one case without the grant that asserts `oops.CodeForbidden`, and one case with it through `authztest.WithExactGrants(t, ctx, authz.NewGrant(scope, resourceID))`.

When the row you loaded carries its organization, state it: `authz.Environment(id, project).Write().InOrganization(authz.OrgIDFrom(row.OrganizationID))`. Take it from the loaded row, never from the session. `Require`, `RequireAny`, `RequireAnyUnblocked`, `Can`, `Evaluate`, `Filter` and `FindMatched` compare it with the organization the caller is acting in and, for now, log an error on a mismatch; that becomes a refusal after the logging window. Helpers that evaluate grants you loaded yourself do not compare it. Stating it is optional until a package moves to typed refs, and an org check needs none: its resource is the organization. A typed id proves an id is well formed, not that the resource is the caller's, so keep a handler test in which a caller from another project names the resource (`environments/tenant_binding_test.go`).

## Adding or changing a permission

1. Edit `registry.Definitions` in `authz/internal/registry/definitions.go`: a `Permission` on the right type (with `Label`, `Description`, `Implies`, `Exclusions`, `Agent`, `Source`), or a new `ResourceType`. Add it to `SystemRoles[authz.SystemRoleAdmin]` too, and to member only if every user should have it. The generator refuses an invalid registry, and `TestDefinitions_IsValid` fails on one. A new permission's scope is `<kind>:<action>`, so leave `LegacyScope` empty. Only the three permissions that predate the registry (`plugin:write`, `log:read`, `mcp:auth`) set it, and they keep it until those scopes are deliberately renamed; validation rejects it anywhere else.
2. `mise run gen:authz`. This rewrites `scopes_gen.go`, `refs_gen.go` and `tables_gen.go`, and `TestGeneratedFilesAreCurrent` fails until you run it. The diff of `tables_gen.go` is the review surface: it shows exactly which scopes now satisfy, block or are managed by what.
3. Update the snapshots from `server/`, then review their diffs, because they are the change:
   - `go test ./internal/authz -run DocTable -args -update-docs` (writes `docs/rbac.md`)
   - `go test ./internal/access -run TestListScopes_DashboardFixture -args -update` (writes the dashboard's `listScopes.fixture.json`)
4. `mise run gen:goa-server`, then `mise run gen:sdk`. The design's scope enum reads `authz.ScopeNames()`.
5. The dashboard reads its tables from `client/dashboard/src/lib/authz.gen.ts`, which `gen:authz` writes, and its matcher in `client/dashboard/src/hooks/useRBAC.ts` is checked against the server by `useRBAC.server-cases.test.ts`, which replays `authz-cases.gen.json` (sets of effective grants with the server evaluator's answers, also written by `gen:authz`). If that test fails after a change, fix the matcher in `useRBAC.ts`, never the cases. Fix the "Dashboard Grant Reference" in `docs/rbac.md` if a page's gate changes.
6. Assess Platform MCP (`maintaining-platform-mcp`) and Command K (`maintaining-command-palette`) if the permission gates a user-facing capability.
7. `mise run lint:server`, `mise run test:server ./internal/authz/... ./internal/access/...`, and `aube run -F dashboard test src/hooks/useRBAC` (includes the server-cases parity test).

The invariant suite (`authz/internal/registry/invariants_test.go`) checks every permission automatically: implications hold, nothing else is satisfied, blocks win, and unknown values fail closed. Do not add per-scope tests for those properties.

## Agents

`Agent: AgentUnsafe()` means no agent policy may ever hold the permission. Use it by default. `SafeSince(N)` makes the permission agent-safe from delegated policy version `N`, and policies stored at an older version keep reading it as unsafe. Making a permission agent-safe therefore means adding a delegated policy version in `server/internal/agents/runtimepolicy/delegated_policy.go`: N is `CurrentDelegatedPolicyVersion` + 1. Add the version constants in both `delegated_policy.go` and `runtimepolicy/scopes.go`, and move both `CurrentDelegatedPolicyVersion` and `CurrentRuntimeScopeRegistryVersion` to N; `SafeSince` panics past the runtime registry's current version. The issuer constants (`IssuedDelegatedPolicyVersion`, `AssistantIssuedPolicyVersion`, `InferenceIssuedPolicyVersion`) are separate contracts; raise one only when that issuer should start carrying the permission. Then commit the new version's stored-policy fixture:

```
go test ./internal/agents/runtimepolicy -run TestStoredPolicyFixtures -args -write-stored-policy-fixture=N
```

The flag refuses to overwrite a fixture. Never regenerate an existing one, because those fixtures are what old stored policies are decoded against.

A credential that acts for a user also carries that user's live policy. Today it decides auth-client checks only. The engine logs a warning (`authz check passes the credential's policies but not the live policy of the user it acts for`) for every other check that policy would refuse, and after the logging window it will refuse them. A workload session acts for nobody and is bounded by its owner only.

## Never

- **A tool or disposition goes on a tool call and nowhere else.** `ToolCall` (and `MCPToolCallCheck`) mark the check as a tool call. A check that carries `tool` or `disposition` without being built that way is an invalid check, and so is a tool call you build on any scope but `mcp:connect` (the engine derives the matching `mcp:blocked_connect` check itself). The engine reads that mark, not which dimensions happen to be present, so a tool call can never be evaluated as a connection.
- **A check with a scope the registry does not know, a resource kind that is not its scope's, or a dimension its scope does not declare is an invalid check.** `resource_kind` and `resource_id` are never dimensions.
- **Never build a raw `authz.Check{...}`, or call `authz.EnvironmentCheck` / `MCPCheck` / `MCPToolCallCheck` / `AssistantCheck` / `HooksIngestCheck` / `ChatReadCheck` / `ChatWriteCheck`, in a migrated package.** The `norawauthzchecks` glint rule reports it. Once no raw constructor or `authz.Check{}` literal remains anywhere in a package, add it to `noRawAuthzChecksPackages` in `glint/no_raw_authz_checks.go`. A partial migration leaves the list alone.
- **Never pass a probe to `Require`, or `AnyOf` anywhere but as the single check of `Require` or `Can`.** Both are invalid checks, which map to an unexpected error rather than a 403.
- **Never build a ref with an empty project.** An empty dimension is an invalid check. Take the project from the row you loaded, not from the request, when they can differ.
- **Never decide whether a risk policy applies from `RiskPolicyApplies(...).Evaluate(...).Satisfied`.** Call `authz.EvaluateRiskPolicy(grants, policyID, dims)` and use `.Applies()`. It names the two reasons a policy leaves a caller alone (`RiskPolicyExempt`, `RiskPolicyNotInAudience`); anything else, including grants that could not be read or a policy that could not be evaluated, means the policy applies. The caller decides how to report the error and never uses it to skip the policy: the realtime scanner logs it and scans, `policycore` returns it.
- **Never hand-edit `scopes_gen.go`, `refs_gen.go` or `tables_gen.go`.** Change `registry.Definitions` and regenerate.
- **Never evaluate grants outside `authz`.** The two SQL list queries in `access/queries.sql` (`ListAccessibleMCPServersForUser`, `ListAccessibleSkillsForUser`) mirror the matcher and precedence. If you change either, change both.
- **Never read a `principal_grants` row for authorization without `(expires_at IS NULL OR expires_at > clock_timestamp())`.** Temporary grants expire in place.

## Reference

[reference.md](reference.md) covers the rest: the `access` management API, grant loading and temporary grants, provisioning, scope overrides, auth-client checks, the dashboard's `useRBAC` and `<RequireScope>`, and inspecting a caller's grants.
