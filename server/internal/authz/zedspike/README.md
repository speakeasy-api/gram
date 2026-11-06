# zedspike: the authorization schema as an embedded SpiceDB file

A review-only spike of the adopted design: one SpiceDB-syntax schema file,
compiled with SpiceDB's own compiler, read over today's `principal_grants`
rows. Nothing outside this folder uses it, and it is not meant to merge as is.
The only change outside the folder is `go.mod`/`go.sum` (importing
`github.com/authzed/spicedb` v1.57.0).

What is real and what is a stub:

| Part                                                                   | State                                                                                                                                |
| ---------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| `rbac.zed`, the whole schema                                           | Real. Compiles, passes SpiceDB's type-system validation, and has no developer warning except the allowed style hint.                 |
| Loader: compile, walk into a `Registry`, cross-check the Go tables     | Real.                                                                                                                                |
| Row landing (`Registry.Land`): a grant row read as relationships       | Real.                                                                                                                                |
| Typed call sites (`refs.go`) and the `Client` (policies, intersection) | Real shape, small.                                                                                                                   |
| The in-process evaluator (`RegistryEvaluator`) and `Predicate.SQL`     | Stubs returning `ErrNotImplemented`.                                                                                                 |
| Conformance test                                                       | Real on SpiceDB's side: 36 cases run through SpiceDB's own evaluator over our rows. Our evaluator's side is skipped until it exists. |

## Layout

| File                  | What it holds                                                                                                                                     |
| --------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| `rbac.zed`            | The schema: 14 definitions, 8 partials, 8 caveats. The only place a permission is defined.                                                        |
| `schema.go`           | `go:embed`, `CompileSchema` (SpiceDB `pkg/schemadsl/compiler`), `LoadRegistry` (walks the compiled protos into `Registry`).                       |
| `registry.go`         | The compiled shape: types, relations (grant, direct, block, own block, edge, fact), permission expressions, caveats.                              |
| `catalogue.go`        | `ScopeCatalogue` (description, sensitive, strict, block, delegatable since, selector keys, surface), `Answers`, `Composites`, `UndeclaredScopes`. |
| `edges.go`            | `Edges` (where each edge's value comes from) and `Facts` (relations filled from a row's columns).                                                 |
| `default_roles.go`    | `DefaultRoles` and `DefaultForEveryone`: seed data, kept out of the schema.                                                                       |
| `tables.go`           | The checks SpiceDB cannot make: schema against catalogue, edges, facts, answers, and "every block is subtracted".                                 |
| `rows.go`             | `Registry.Land`: one grant row to the relationships it stands for.                                                                                |
| `refs.go`             | Typed references and methods, the only way to build a `Check`.                                                                                    |
| `client.go`           | `Client`, `Caller`/policies, `Evaluator`, `Lookup`/`Predicate`, stubbed `RegistryEvaluator`.                                                      |
| `example/handlers.go` | Handler-style code for the representative checks.                                                                                                 |
| `schema_test.go`      | Compile, validation, warnings gate, registry/table agreement, typed methods, row landing.                                                         |
| `conformance_test.go` | The behaviour cases, answered by SpiceDB's evaluator; the in-process evaluator must agree.                                                        |

Run everything with `go test ./server/internal/authz/zedspike/...`. No Docker,
no database.

## Reading rbac.zed

- **Relation name = stored scope with `:` as `_`.** `mcp_connect` holds
  `mcp:connect` rows. No mapping table; the loader fails on a relation that
  names no catalogue scope and on a catalogue scope with no relation.
- **`_direct` and `_own`.** `mcp_connect_direct` holds the same rows as
  `mcp_connect` when they were written for one user by name, name this server
  and were not inferred. `mcp_blocked_connect_own` holds blocks written for
  the user being checked. Every blockable permission reads
  `((inherited grants - inherited blocks) + direct grants) - own blocks`,
  which is today's precedence (`precedence.go`) written into the file.
- **Partials.** Each kind's grant set (`mcp_grants`, `environment_grants`, …)
  is a `partial` spliced into the resource, its project and its organization,
  because a row lands on whichever of the three its selector names. Partials
  are plain SpiceDB (`use partial`); without them every grant line appears
  three times.
- **Edges.** `relation project: project` and `relation organization:
  organization` are never stored. Organization edges are always the caller's
  organization; project edges come from the reference (`MCPServer(projectID,
  id)`). Every child type has its own organization edge, so permissions read
  `x + project->x + organization->x`.
- **Subjects.** Allows accept `user | user:* | role#member | agent`, plus
  `api_key` on the four scopes a key policy may hold. `_direct` and `_own`
  accept only `user`.
- **Caveats.** `granted_*` parameters come from the row's selector (missing
  or `"*"` is `""`); the rest come from the check, and typed methods always
  pass them. Missing context returns conditional, which the client denies.
- **Building blocks.** Permissions starting with `has_` or `blocked_` cannot
  be named by a typed method.

## How it maps to today's rows

Storage does not change and no SpiceDB server runs. `Registry.Land` reads each
`principal_grants` row as relationships, in memory:

| Row column                                                                                          | Becomes                                                                                          | Example                                                    |
| --------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------ | ---------------------------------------------------------- |
| `principal_urn` `user:dana` / `user:all` / `role:organization:<id>` / `agent:<id>` / `api_key:<id>` | subject `user:dana` / `user:*` / `role:organization_<id>#member` / `agent:<id>` / `api_key:<id>` |                                                            |
| `scope`                                                                                             | relation, `:` as `_`, plus `_direct` / `_own` by principal                                       | `mcp:connect` → `mcp_connect`                              |
| `selectors.resource_id` concrete                                                                    | object is that resource                                                                          | `mcp_server:S#mcp_connect@…`                               |
| `resource_id "*"` with `project_id`                                                                 | object is the project                                                                            | `project:P#mcp_connect@…`                                  |
| `resource_id "*"` alone                                                                             | object is the organization                                                                       | `organization:O#mcp_connect@…`                             |
| other selector keys                                                                                 | caveat `granted_*` parameters                                                                    | `tool: search` → `mcp_tools_allow{granted_tool: "search"}` |
| `inferred`                                                                                          | keeps a user's concrete grant out of `_direct`                                                   |                                                            |

Role assignments become `role:<id>#member@user:<id>`. Edges and facts come
from the check, never from storage (`edgeRelationships` in the conformance
test shows them as the relationships SpiceDB would need).

`TestLandRows` pins these rules.

## Call sites

```go
s.authz.Require(ctx, authz.Organization(orgID).Administer())
s.authz.Require(ctx, authz.MCPServer(server.ProjectID, server.ID).CallTool(tool, disposition))
s.authz.Require(ctx, authz.Chat(chat.ProjectID, chat.ID).WithOwner(owner).Read())
predicate, err := s.authz.LookupResources(ctx, authz.Project(orgID, projectID).ReadableMCPServers())
```

The caller is never passed. `Client` reads it from the context admission
filled, evaluates the schema once per policy behind it (person; or credential
ceiling, agent and owner) and requires every one to allow. That is the agent
intersection; SpiceDB's `&` cannot express it because it intersects two
permissions of one subject. A direct grant overriding a block and an agent
capped by its owner need no handler code at all; see `example/handlers.go`.

## The robustness review's eight changes, and where each is

1. **Typed method calls, no request struct.** `refs.go`. `Check` has only
   unexported fields, so no code outside the package can build one.
2. **Composites as plain unions.** `plugin_publication`,
   `read_or_organization_read`, `write_or_administer` and
   `project_read_or_edit` are permissions in the file. `RequireAny`,
   `AnyOf`, `DecideAlternatives` and `AnyBlockVetoes` have no counterpart.
   `plugin_publication` keeps today's veto by subtracting the plugin block
   from both branches.
3. **Explicit caveats, string parameters.** `mcp_tools_allow` (permissive),
   `mcp_tools_block` (strict), plus a third, `mcp_tools_direct`; see
   simplifications below.
4. **Migrate project-ID rows.** Described, not run (below). The collection
   checks become project permissions (`Project(o, p).ReadMCPServers()`,
   `CreateMCPServer()`, `ReadEnvironments()`, …), replacing the `*InProject`
   references and the `project_or_skill_read` / `project_skills` path.
   `Land` refuses a row whose `resource_id` equals its `project_id`.
5. **Precedence via `_direct` / `_own`.** In the file; 15 `_direct` and 15
   `_own` relations. Conformance cases pin every variation from the
   walkthrough (broad grant, inferred, agent, tool-limited, own block,
   `user:all` block).
6. **Subject types per relation; `root` kept.** `user:*`, `agent`, `api_key`
   listed. `root` is a relation on organization reached from every `has_*`,
   marked as a deletion candidate.
7. **Explicit `answers`.** `Answers` in `catalogue.go`; reordering a union
   never changes the old tables. Organization edges are one implicit rule,
   so `Edges` lists only the six project edges.
8. **Conformance test.** `conformance_test.go`, against SpiceDB's own
   development evaluator, over rows landed by our loader. The data-impact
   diff over shadow grants and the defaults-change test are not in the spike.

## Simplifications and stubs

Stubs:

- `RegistryEvaluator.Check` and `.LookupResources`, `Predicate.SQL` and
  `intersect` return or panic with `ErrNotImplemented`. The conformance test
  skips our side until they exist.
- No grant loading: `Policy.Grants` is filled by the test. Production keeps
  `LoadGrants`.
- `example/handlers.go` uses stand-in stores and compiles but is not run.
- `MCPServerOf(row)`-style constructors (taking the loaded row so the project
  cannot come from the request) are only described in a comment.
- Agent and workload resource scopes (`agent:*`, `workload:*`) stay in
  `UndeclaredScopes`, as on int.
- The data migration for project-ID rows is not written.

Choices made without a reviewer, to confirm:

- **Partials** instead of repeating each grant set three times. Plain
  SpiceDB, but a newer feature; expanding them is mechanical if unwanted.
- **Three MCP caveats, not two.** Today a direct grant proves server-level
  access whatever tool it names, but overrides a block on a tool call only
  when it names everything the call carries. Neither the allow nor the block
  semantics fit, so `mcp_tools_direct` exists. Upstream auth clients use one
  strict caveat for allows and blocks, as today.
- **Blocks land at every level.** A block with `resource_id "*"` is valid
  today, so block relations exist on organization and project too (via the
  partials) and every `blocked_*` building block has three terms.
- **`agent` on every relation.** `agents/runtimepolicy` admits every active
  scope in an agent's direct policy; `DelegatableSince` only limits what an
  owner may delegate, which is a write-time check. The schema therefore
  cannot carry delegation; the catalogue does.
- **`api_key` only on `mcp:connect`, `mcp:blocked_connect`, `mcp:auth`,
  `mcp:blocked_auth`**, matching `apiKeyPolicyScopes`.
- **`role#member` accepts only users.** Agents holding roles would need
  `agent` added there.
- **`$represented_user`** is matched inside `auth_client_match` against a
  `represented_user` parameter the client fills from the caller.
- **Rows without `project_id` whose `resource_id` is a project ID** (the
  `skill:read` rows written by `assistantidentity/provision.go`) cannot be
  detected by the loader; they would land on `skill:<project id>` and match
  nothing. This is why the migration is a prerequisite, not an option.
- **Chats take a project.** `Chat(projectID, chatID)`, so the project edge
  comes from the reference like every other child type.
- **`risk_policy` has no organization edge**: its rows name one policy, as
  today. A `"*"` risk-policy row would fail to land.

Simplifications this exposes (not applied):

- **Delete `root`**: one relation and 14 `organization->root` terms.
- **Option D, blocks always win**: removes all 30 `_direct`/`_own`
  relations and the `mcp_tools_direct` caveat, and halves every blockable
  permission line. Needs a production count of direct exceptions first.
- **Drop project- and organization-level blocks** if production has no
  wildcard block rows: the `*_blocked_*` relations then live only on the
  resource, and every `blocked_*` building block becomes one term.
- **Arrows to parent permissions** (`project->has_mcp_read`) instead of a
  direct organization edge per child type: SpiceDB's idiom, silences the one
  allowed warning, and removes the `organization->x` term from every child
  permission. It reverses the 2026-11-06 decision, so only noted here.
- **`apiKeyPolicyScopes` and the agent "active" scope list** could be read
  from the schema's subject types instead of kept as Go allowlists.
- **`chat#read_grant` / `write_grant`** exist only for the old tables'
  answers and can go once those retire.
- **`selector.go`'s `allowedSelectorKeys` and `ResourceKindForScope`** are
  derivable from the catalogue's `Selectors` and the scope-to-type map in
  `rows.go`.

## Dependencies

The production path imports only `pkg/schemadsl/compiler`, `pkg/proto/core/v1`
and `pkg/schemadsl/input`. The tests also import `pkg/development` and
`pkg/tuple` for SpiceDB's evaluator and warnings. Go's minimum version
selection raises 86 existing requirements and adds about 46 new ones (pgx, grpc and otel among the upgrades),
which is why `go.mod` changes in the same commit.
