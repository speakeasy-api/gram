---
name: gram-rbac
description: Use when a task touches authorization in Speakeasy — gating a handler or list endpoint, `authz.Require` / `RequireAny` / `RequireAnyUnblocked` / `Can` / `Evaluate` / `Filter`, typed refs like `authz.MCPServer(id, project).Write()` or `authz.Project(p).Environments().Create()`, `authz.Check{...}` literals, `authz.EnvironmentCheck` / `MCPCheck` / `MCPToolCallCheck`, adding or changing a scope, permission, resource type, block or dimension in `authz.Model` (`registry_resources.go`), system role defaults, agent-safe scopes (`SafeSince`, `AgentUnsafe`), `mise run gen:authz`, `refs_gen.go`, `scopes_gen.go`, `registry_tables.golden.yaml`, the `norawauthzchecks` lint, "invalid authz check", `ErrInvalidCheck`, `CodeForbidden` from authz, or RBAC state in the dashboard (`useRBAC`, `RequireScope`, access pages).
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

The whole permission model is one Go value, `authz.Model` in `server/internal/authz/registry_resources.go`. The scope constants (`scopes_gen.go`), the typed request refs (`refs_gen.go`; the project collections and `ToolCall` are hand-written in `refs.go`), the expansion and block tables, the selector key allowlists, the scope catalogue, the scope enum in the Goa design and the `docs/rbac.md` scope table are all derived from it. One server-side evaluator, `authz.Engine`, is canonical. Two list queries still mirror most of its matching and precedence in SQL (`ListAccessibleMCPServersForUser`, `ListAccessibleSkillsForUser`); a change to matching or blocks must update them too, until the SQL prefilter replaces them. They are not exact mirrors: the skill list leaves out a skill whose only grant is `skill:write` blocked by `skill:blocked_write`, although the engine still allows `skill:read` on it (kept on purpose; see the query's comment). **Change the model, regenerate, and let the tests tell you what else moved.** Never hand-edit a derived table.

## Mental model

| Term             | Meaning                                                                                                                                                                                                                  |
| ---------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Resource type    | A `ResourceType` in the model: `Kind` (`mcp`, `environment`, `project`, ...), optional `Parent` (`project`), its `Dimensions`, and its `Permissions`.                                                                    |
| Permission       | One action on a type (`read`, `write`, `connect`). Its scope is `<kind>:<action>`; `Implies` lists the actions a grant of it also satisfies.                                                                             |
| Block            | A permission's `Exclusions: Blocked(...)` generate `<kind>:blocked_<action>`. A block also takes away every permission that implies it.                                                                                  |
| Grant            | Scope + selector held by a principal (user, role, `user:all`, agent, API key). Build with `authz.NewGrant` / `NewGrantWithSelector`.                                                                                     |
| Check            | What a request asks for. Build it with a typed ref, never by hand.                                                                                                                                                       |
| Dimension values | A concrete value matches an equal grant value or `*`. `authz.SelectorValueUnknown` (`$unknown`) never satisfies a constrained allow and always matches a constrained block. An empty value or `*` in a check is invalid. |
| Precedence       | A user's direct grant naming a concrete resource outranks a block inherited from a role or `user:all`. Lives in `precedence.go`.                                                                                         |

## Gating a handler

```go
// one resource
if err := s.authz.Require(ctx, authz.MCPServer(authz.MCPServerID(id), authz.ProjectID(project)).Write()); err != nil {
	return err
}
// no resource yet (create), or a lookup that missed (do not leak existence)
if err := s.authz.Require(ctx, authz.Project(authz.ProjectID(p)).Environments().Create()); err != nil {
	return err
}
if err := s.authz.Require(ctx, authz.Project(authz.ProjectID(p)).Environments().Read()); err != nil {
	return err
}
// a project-wide grant OR a grant on the resource, and a block on either refuses
if err := s.authz.Require(ctx, authz.AnyOf(authz.Project(authz.ProjectID(p)).Write(), authz.Skill(authz.SkillID(id)).Write())); err != nil {
	return err
}
// a branch, not a denial (no challenge logged)
ok, err := s.authz.Can(ctx, authz.Chat(authz.ChatID(id)).Read())
// discovery: "could they act on any matching resource?"
ok, err := s.authz.Can(ctx, authz.MCPServer(authz.MCPServerID(id), authz.ProjectID(p)).Connect().Probe())
```

| Need                                             | Use                                                                                                                                                                    |
| ------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Refuse unless allowed                            | `Require(ctx, checks...)` (all must pass)                                                                                                                              |
| Several equivalent grants, blocks must still win | `Require(ctx, authz.AnyOf(...))` (same as `RequireAnyUnblocked`)                                                                                                       |
| Branch on the answer                             | `Can(ctx, ...)`. Accepts a single `AnyOf` and probes. Returns an error, not false, for an invalid check.                                                               |
| Narrow a list                                    | `Filter(ctx, checks)` returns the allowed `ResourceID`s. `FindMatched` returns a bool per check, for many checks that share one resource id (tools in a `tools/list`). |
| MCP tool call                                    | `authz.MCPServer(id, p).ToolCall(tool, disposition)`. An empty disposition becomes unknown, so disposition blocks apply to unannotated tools.                          |

Ref constructors take named string types (`authz.ProjectID`, `authz.EnvironmentID`, `authz.MCPServerID`, ...), so convert a uuid with `authz.EnvironmentID(id.String())`. A project collection (`Project(p).Environments()`, `.MCPServers()`, `.Assistants()`) builds a check on the resource type whose resource id is the project id, exactly what `EnvironmentCheck(scope, p, p)` built before, so existing grants match it the same way and `Filter` returns the project id for it.

When migrating a package from the old constructors:

| Before                                      | After                                                                                   |
| ------------------------------------------- | --------------------------------------------------------------------------------------- |
| `EnvironmentCheck(s, envID, p)`             | `authz.Environment(authz.EnvironmentID(envID), authz.ProjectID(p)).Read()` / `.Write()` |
| `EnvironmentCheck(s, p, p)`                 | `authz.Project(authz.ProjectID(p)).Environments().Read()` / `.Write()` / `.Create()`    |
| `MCPCheck(ScopeMCPWrite, p, p)`             | `authz.Project(authz.ProjectID(p)).MCPServers().Create()`                               |
| `AssistantCheck(ScopeAssistantWrite, p, p)` | `authz.Project(authz.ProjectID(p)).Assistants().Create()`                               |
| `MCPToolCallCheck(...)`                     | `authz.MCPServer(id, p).ToolCall(tool, disposition)`                                    |

Keep `Require` and its `CodeForbidden` branch where the code already has one. Swapping it for `Can` keeps enforcement the same but stops logging the challenge, so do that only where the denial is not a refusal the user sees.

Refs exist for every kind in the model except `mcp_auth_client` (`MCPAuthClientCheck`), `risk_policy` (`RiskPolicyApplies` / bypass checks) and `inference` (`InferenceExecuteCheck`). The list sits at the end of `refs_gen.go`.

For a list endpoint, load the candidate rows, build one ref per row, call `Filter`, and rebuild the response from the ids it returns. Never call `Require` per item.

In the handler's test, add one case without the grant that asserts `oops.CodeForbidden`, and one case with it through `authztest.WithExactGrants(t, ctx, authz.NewGrant(scope, resourceID))`.

## Adding or changing a permission

1. Edit `authz.Model` in `registry_resources.go`: a `Permission` on the right type (with `Label`, `Description`, `Implies`, `Exclusions`, `Agent`), or a new `ResourceType`. Add it to `SystemRoles[SystemRoleAdmin]` too, and to member only if every user should have it. `Model.Validate` runs when the package loads, so a bad edit fails every test.
2. `mise run gen:authz`. This rewrites `scopes_gen.go` and `refs_gen.go`, and `TestGeneratedFilesAreCurrent` fails until you run it.
3. Update the snapshots from `server/`, then review their diffs, because they are the change:
   - `go test ./internal/authz -run TestModel_DerivedTablesGolden -args -update` (writes `testdata/registry_tables.golden.yaml`)
   - `go test ./internal/authz -run DocTable -args -update-docs` (writes `docs/rbac.md`)
   - `go test ./internal/access -run TestListScopes_DashboardFixture -args -update` (writes the dashboard's `listScopes.fixture.json`)
4. `mise run gen:goa-server`, then `mise run gen:sdk`. The design's scope enum reads `authz.ScopeNames()`.
5. The dashboard still mirrors some tables by hand in `client/dashboard/src/hooks/useRBAC.ts` (`resourceKindForScope`, `exclusionScopesByScope`). Update them, and fix the "Dashboard Grant Reference" in `docs/rbac.md` if a page's gate changes.
6. Assess Platform MCP (`maintaining-platform-mcp`) and Command K (`maintaining-command-palette`) if the permission gates a user-facing capability.
7. `mise run lint:server` and `mise run test:server ./internal/authz/... ./internal/access/...`.

The invariant suite (`registry_invariants_test.go`) checks every permission automatically: implications hold, nothing else is satisfied, blocks win, and unknown values fail closed. Do not add per-scope tests for those properties.

## Agents

`Agent: AgentUnsafe()` means no agent policy may ever hold the permission. Use it by default. `SafeSince(N)` makes the permission agent-safe from delegated policy version `N`, and policies stored at an older version keep reading it as unsafe. Making a permission agent-safe therefore means adding a delegated policy version in `server/internal/agents/runtimepolicy/delegated_policy.go`: N is `CurrentDelegatedPolicyVersion` + 1. Add the version constants in both `delegated_policy.go` and `runtimepolicy/scopes.go`, and move both `CurrentDelegatedPolicyVersion` and `CurrentRuntimeScopeRegistryVersion` to N; `SafeSince` panics past the runtime registry's current version. The issuer constants (`IssuedDelegatedPolicyVersion`, `AssistantIssuedPolicyVersion`, `InferenceIssuedPolicyVersion`) are separate contracts; raise one only when that issuer should start carrying the permission. Then commit the new version's stored-policy fixture:

```
go test ./internal/agents/runtimepolicy -run TestStoredPolicyFixtures -args -write-stored-policy-fixture=N
```

The flag refuses to overwrite a fixture. Never regenerate an existing one, because those fixtures are what old stored policies are decoded against.

## Never

- **Never build a raw `authz.Check{...}`, or call `authz.EnvironmentCheck` / `MCPCheck` / `MCPToolCallCheck` / `AssistantCheck` / `HooksIngestCheck` / `ChatReadCheck` / `ChatWriteCheck`, in a migrated package.** The `norawauthzchecks` glint rule reports it. Once no raw constructor or `authz.Check{}` literal remains anywhere in a package, add it to `noRawAuthzChecksPackages` in `glint/no_raw_authz_checks.go`. A partial migration leaves the list alone.
- **Never pass a probe to `Require`, or `AnyOf` anywhere but as the single check of `Require` or `Can`.** Both are invalid checks, which map to an unexpected error rather than a 403.
- **Never build a ref with an empty project.** An empty dimension is an invalid check. Take the project from the row you loaded, not from the request, when they can differ.
- **Never hand-edit `scopes_gen.go`, `refs_gen.go`, the golden YAML, or the derived maps in `scopes.go`.** Change the model.
- **Never evaluate grants outside `authz`.** The two SQL list queries in `access/queries.sql` (`ListAccessibleMCPServersForUser`, `ListAccessibleSkillsForUser`) mirror the matcher and precedence. If you change either, change both.
- **Never read a `principal_grants` row for authorization without `(expires_at IS NULL OR expires_at > clock_timestamp())`.** Temporary grants expire in place.

## Reference

[reference.md](reference.md) covers the rest: the `access` management API, grant loading and temporary grants, provisioning, scope overrides, auth-client checks, the dashboard's `useRBAC` and `<RequireScope>`, and inspecting a caller's grants.
