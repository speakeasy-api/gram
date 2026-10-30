# RBAC options B and D, side by side

This document compares two candidate designs for refactoring authorization, using the same five MCP server scenarios for both. It is assembled from each option's step-by-step guide and does not recommend either option.

- **Option B, the scope catalogue.** Code lives in `server/internal/authz/catalog/`, on branch [`claude/project-thread-p7id3b`](https://github.com/speakeasy-api/gram/tree/claude/project-thread-p7id3b/server/internal/authz/catalog). Source guide: [`catalog/GUIDE.md`](https://github.com/speakeasy-api/gram/blob/c7b47a9a37978e0f39bd757808cfee9470c4c560/server/internal/authz/catalog/GUIDE.md) at `c7b47a9a3`.
- **Option D, the declarative relationship schema.** Code lives in `server/internal/authz/declarative/`, on branch [`claude/project-thread-w79i14`](https://github.com/speakeasy-api/gram/tree/claude/project-thread-w79i14/server/internal/authz/declarative). Source guide: [`declarative/GUIDE.md`](https://github.com/speakeasy-api/gram/blob/0bd5c29530b059f0d433b3f9288ff5fd0113fcea/server/internal/authz/declarative/GUIDE.md) at `0bd5c2953`.

File references are `file:line` on each option's branch, under `server/internal/`. Labels used throughout:

- **Implemented** means the code is on the branch and has tests.
- **Sketched** means proposed code that is not wired in or not tested.

## Contents

1. [The short version](#1-the-short-version)
2. [How a permission is defined](#2-how-a-permission-is-defined)
3. [What an authorization request looks like](#3-what-an-authorization-request-looks-like)
4. [The data model both options read](#4-the-data-model-both-options-read)
5. [The five MCP scenarios](#5-the-five-mcp-scenarios)
6. [More scenarios: projects, agents, time-bound access](#6-more-scenarios-projects-agents-time-bound-access)
7. [Legacy broad grants](#7-legacy-broad-grants)
8. [Shared gaps and open questions](#8-shared-gaps-and-open-questions)

---

## 1. The short version

| | Option B: scope catalogue | Option D: relationship schema |
| --- | --- | --- |
| Unit of definition | One `Definition` per stored scope (`mcp:connect`) | Types, relations and permissions, declared with builders (`mcp_server.connect`) |
| What a handler asks for | A stored scope on a target: `Request{Permission: mcp:connect, Target}` | A named permission on a typed reference: `MCPServerConnect` on `MCPServerRef` |
| Who decides | The existing engine matches grants. The catalogue validates the request first and labels the result after. | The declarative package evaluates the grants itself. The engine only loads grants and logs the decision. |
| Implication ("manage implies view") | `ImpliedBy` list on the definition, mirrored in the engine's tables and checked by a test | A union inside the permission: `read = viewer ∪ editor ∪ root` |
| Blocks | `ExcludedBy` + `BlockWins` on the definition, mirrored in the engine | `s.Block(...)` + `s.ExcludedBy(...)` on the permission: always "allows minus blocks" |
| Where a grant may attach | `AllowedAnchors`. A broader row is still allowed, but labelled legacy | `s.AnchoredAt` (allowed) and `s.LegacyAnchoredAt` (honored, labelled legacy). Any other anchor fails decoding |
| Rollout | Applies engine-wide for MCP scopes on the branch | Per organization, through the `MCPServerSchemaActive` switch (off everywhere today) |
| New storage | None. Reads `principal_grants` unchanged | None. Reads `principal_grants` unchanged |
| Project blocks vs a direct user grant | Today's rule kept: the direct grant wins (`BlockWins: false`) | The block always wins |
| Agents (R ∩ A ∩ O) | Unchanged engine loop over the three policies | `evaluatePolicies` requires every policy to allow |
| Relationship rules (agent owner reads history) | Sketched: hand-written `Relations` enum on a definition | **Implemented** at `93da99dc4`: `Attr`, `Is`, `Via`, `Unless` in the schema language, behind the per-org switch |
| Time-bound grants | Unsupported; sketched | Unsupported; sketched |
| Tool-level checks | Still in the engine, outside the catalogue (`MCPToolCall` builder sketched) | Evaluator implemented and tested; tool-call entry point (`RequireMCPTool`) sketched |
| Safety net at startup | `mustValidate` panics on a bad definition; a test keeps the engine's tables equal to the catalogue | `relschema.Compile` panics on a bad schema. Since `93da99dc4` an unmapped registered scope fails startup unless it's listed in `outOfSchemaScopes` with a reason. Before that, the wiring excluded such scopes automatically, so the check couldn't fail |

---

## 2. How a permission is defined

### Option B

Each permission the code can check is one `Definition` in a single list (`authz/catalog/permissions.go:11`, list at `:39`). The name **is** the stored scope.

```go
type Definition struct {
	Name              authz.Scope   // the stored scope, e.g. "mcp:connect"
	Resource          string        // the kind of thing it protects, e.g. "mcp"
	AllowedAnchors    []string      // where a grant may point: one resource, a project, the org
	RequiredAncestors []string      // containers the target must name, e.g. project and org
	ImpliedBy         []authz.Scope // other permissions that also satisfy it, one step, explicit
	ExcludedBy        authz.Scope   // the block that withdraws it
	BlockWins         bool          // a block beats every allow, even a direct grant to the user
}
```

The three MCP server permissions:

```go
var definitions = mustValidate([]Definition{
	{
		Name:              authz.ScopeMCPRead,
		Resource:          authz.ResourceKindMCP,
		AllowedAnchors:    []string{authz.ResourceKindMCP, authz.ResourceKindProject, authz.ResourceKindOrg},
		RequiredAncestors: mcpServerAncestors, // {project, org}
		ImpliedBy:         []authz.Scope{authz.ScopeMCPWrite},
		ExcludedBy:        authz.ScopeMCPBlockedRead,
		BlockWins:         true,
	},
	{
		Name:              authz.ScopeMCPWrite,
		Resource:          authz.ResourceKindMCP,
		AllowedAnchors:    []string{authz.ResourceKindMCP, authz.ResourceKindProject, authz.ResourceKindOrg},
		RequiredAncestors: mcpServerAncestors,
		ImpliedBy:         nil,
		ExcludedBy:        authz.ScopeMCPBlockedWrite,
		BlockWins:         true,
	},
	{
		Name:              authz.ScopeMCPConnect,
		Resource:          authz.ResourceKindMCP,
		AllowedAnchors:    []string{authz.ResourceKindMCP}, // a connect grant names one server
		RequiredAncestors: mcpServerAncestors,
		ImpliedBy:         nil,
		ExcludedBy:        authz.ScopeMCPBlockedConnect,
		BlockWins:         true,
	},
})
```

The engine keeps its own copy of the matching rules (`authz/scopes.go:284`, `:342`, `authz/precedence.go:51`), because the engine cannot import the catalogue. `TestDefinitionsMatchEngine` (`authz/catalog/client_test.go:69`) fails if the two disagree. At startup, `mustValidate` panics on a duplicate, a block used as a permission, a resource kind that doesn't match the scope, an implication across kinds, or a missing block.

### Option D

A permission is declared once in a schema, as a computed expression over **relations**, and each relation says which stored scope feeds it (`declarative/mcp_server_schema.go:43`):

```go
root := s.AncestorRel(org, "root")
anywhere := s.AnchoredAt(server, project, org)
toolLimited := s.ConstrainedBy(authz.SelectorKeyTool, authz.SelectorKeyDisposition)

s.NewType(org, s.StoredKind(authz.ResourceKindOrg),
	s.Grant("root", s.StoredAs(string(authz.ScopeRoot)), s.StoredKindAs(authz.ResourceKindWildcard), s.AnchoredAt(org)),
),
s.NewType(project, s.Parent(org), s.StoredKind(authz.ResourceKindProject)),
s.NewType(server, s.Parent(project), s.StoredKind(authz.ResourceKindMCP),
	s.Grant("viewer", s.StoredAs(string(authz.ScopeMCPRead)), anywhere, s.NarrowableBy(project)),
	s.Grant("editor", s.StoredAs(string(authz.ScopeMCPWrite)), anywhere, s.NarrowableBy(project)),
	// A connection grant names one server. Project- and
	// organization-wide rows are honored until they are migrated.
	s.Grant("connector", s.StoredAs(string(authz.ScopeMCPConnect)), s.AnchoredAt(server), s.LegacyAnchoredAt(project, org),
		s.NarrowableBy(project), toolLimited),

	s.Block("read_blocked", s.StoredAs(string(authz.ScopeMCPBlockedRead)), anywhere, s.NarrowableBy(project)),
	s.Block("edit_blocked", s.StoredAs(string(authz.ScopeMCPBlockedWrite)), anywhere, s.NarrowableBy(project)),
	s.Block("connect_blocked", s.StoredAs(string(authz.ScopeMCPBlockedConnect)), anywhere, s.NarrowableBy(project), toolLimited),

	s.Permission("read", s.Union(s.Rel("viewer"), s.Rel("editor"), root), s.ExcludedBy("read_blocked")),
	s.Permission("edit", s.Union(s.Rel("editor"), root), s.ExcludedBy("edit_blocked")),
	// No edge from read or edit: configuration authority is not use.
	s.Permission("connect", s.Union(s.Rel("connector"), root), s.ExcludedBy("connect_blocked")),
),
```

Code asks for permissions through typed constants (`MCPServerRead`, `MCPServerEdit`, `MCPServerConnect`), so a call site can't pass a raw scope string. The schema compiles once at startup (`var mcpServerSchema = mustCompileMCPServerSchema()`). `relschema.Compile` refuses an unknown parent, a chain that doesn't end at the organization, an anchor outside the type's ancestry, a permission naming an unknown relation, a stored scope mapped twice, and **a registered scope that the schema neither reads nor declares out of scope**.

### The same idea in each

| Concept | B | D |
| --- | --- | --- |
| "Managing implies viewing" | `mcp:read` has `ImpliedBy: [mcp:write]` | `read` includes `s.Rel("editor")` |
| "Managing does not imply connecting" | `mcp:connect` has `ImpliedBy: nil` | `connect` doesn't include `editor` |
| "Connect names one server" | `AllowedAnchors: [mcp]` | `s.AnchoredAt(server), s.LegacyAnchoredAt(project, org)` |
| "A block withdraws connect" | `ExcludedBy: mcp:blocked_connect`, `BlockWins: true` | `s.ExcludedBy("connect_blocked")` |
| "A connect row may limit tools" | Engine table: `tool`, `disposition` allowed on `mcp` grants (`authz/selector.go:186`) | `toolLimited` on `connector` and `connect_blocked` |
| "Grants on the project apply to its servers" | Engine's selector match on `project_id` | `s.Parent(project)` plus the anchor rule |

---

## 3. What an authorization request looks like

### Option B

A handler builds a `Target` from rows it loaded, turns it into `Request`s with per-resource builders, and hands them to a `catalog.Client` (`authz/catalog/client.go`, `mcpserver.go`):

```go
type ResourceRef struct{ OrganizationID, Kind, ID string }
type Target struct {
	Resource  ResourceRef
	Ancestors []ResourceRef // containers it actually sits in, from loaded rows
}
type Request struct {
	Permission authz.Scope
	Target     Target
}
type Decision struct {
	Outcome Outcome // allowed, denied, unresolved (a fact was missing; never allows)
	Reason  Reason  // allowed, no_grant, blocked, cross_tenant, missing_fact, not_enforced
	Basis   Basis   // set when an allow rested on an old, broader grant
	Request Request // the first request that was not allowed
	Grants  []authz.Grant
}
```

The private endpoint proxy (`mcp/serveendpoint.go:1056`, `:1077`, `:1092`):

```go
target := catalog.MCPServerTarget(organizationID, projectID.String(), mcpServer.ID.String())
requests, err := catalog.MCPServerUse(target, catalog.MCPServerVisibility(mcpServer.Visibility))
decision, err := catalog.NewEngineClient(s.authz).Require(ctx, requests...)
```

Two calls: `Require` enforces (403 and a challenge log on failure), `Authorize` answers "could I?" for the UI without recording. Both use the same request builder.

The client is built from the engine each service already receives (`catalog.NewEngineClient(authzEngine)`, kept as a field in `remotemcp/impl.go:96`), so no constructor signature or `start.go` change.

On every call the client:

1. **Validates** (`client.go:162`, `prepare`): the permission has a definition, the target is one concrete resource (not `*`), its organization is the caller's (else `cross_tenant`), and every required ancestor is present and in the same organization (else `missing_fact`). No grant is read yet.
2. **Evaluates** through the backend, which calls `authz.Engine.Require` with a check built from the target.
3. **Decides** (`client.go:229`): allowed, or the first failed request and why. An allow from a row broader than `AllowedAnchors` is labelled `legacy_broad_grant`.

```
handler ──Target, Requests──▶ catalog.Client ──validated check──▶ catalog.Backend ──▶ authz.Engine (matches principal_grants)
```

### Option D

A handler builds a typed reference from its own tenant-scoped row and calls one function with the engine it already holds (`declarative/mcp_server_access.go:54`):

```go
server := declarative.MCPServerRef{
	ID:             row.ID.String(),
	ProjectID:      row.ProjectID.String(),
	OrganizationID: organizationID,
}
if err := declarative.RequireMCPServer(ctx, s.authz, declarative.MCPServerConnect, server); err != nil {
	return err // already a 403, logged like any other denial
}
```

`MCPServerCapabilities(ctx, s.authz, server)` returns `{Read, Edit, Connect}` for API responses from the same decision function. There is no new client: the declarative package uses small hooks on the existing engine (`authz/policy_set.go`): `PolicySet`, `MCPServerSchemaActive`, `RecordDecision`, `MapError`, `IsAPIKeyGrant`.

On every call `RequireMCPServer`:

1. Asks `engine.MCPServerSchemaActive(ctx)`. If the organization is off, it runs the legacy `engine.Require` unchanged (`:65`).
2. Fetches `engine.PolicySet(ctx)`: the caller's grants, grouped by policy, bound to the organization they were loaded for.
3. `decideMCPServer` (`:144`) builds the object (`mcp_server:S1` with ancestors `project:PA`, `organization:A`), decodes each row into a relationship (`relschema/decode.go:80`) and evaluates the permission (`relschema/eval.go:72`), with a trace.
4. `engine.RecordDecision` writes the same challenge log as `Require` and returns nil or the same 403.

```
handler ──MCPServerRef, permission──▶ declarative.RequireMCPServer ──▶ engine.PolicySet ──▶ relschema.Decode + Evaluate ──▶ engine.RecordDecision
```

### Side by side

| | B | D |
| --- | --- | --- |
| Call | `client.Require(ctx, catalog.MCPServerUse(target, vis)...)` | `declarative.RequireMCPServer(ctx, engine, MCPServerConnect, ref)` |
| Object passed | `Target` (resource + ancestors), built from loaded rows | `MCPServerRef` (id, project, org), built from the loaded row |
| What is named | A stored scope (`mcp:connect`) via a builder | A permission constant (`MCPServerConnect`) |
| "Can I?" for the UI | `client.Authorize(ctx, requests...)` | `MCPServerCapabilities(ctx, engine, ref)` |
| Tenant check | `prepare` compares the target's org with the active org | `Evaluate` compares the object's org with the policy's org |
| Answer | `Decision{Outcome, Reason, Basis, ...}` | `relschema.Decision` (allowed, denied, undetermined) with a trace |
| Grant matching | The engine's existing selector match | The declarative evaluator; the engine doesn't match |

---

## 4. The data model both options read

Neither option adds a table or a column. Both read the existing `principal_grants` rows that the engine loads for the caller (`authz/engine.go:178` resolves principals, `:196` and `authz/load.go:14` load rows). Role assignment is not a grant row: when a request arrives, the user is resolved to their user principal and one principal per assigned role, and every row for those principals is loaded.

The columns that matter here:

| Column | Meaning |
| --- | --- |
| `organization_id` | The tenant that owns the row |
| `principal_urn` | Who holds it: a user or a role, e.g. `role:organization:role-a` |
| `scope` | The stored permission or block, e.g. `mcp:connect`, `mcp:blocked_connect` |
| `selectors` | JSON describing what it applies to: `resource_kind`, `resource_id` (`*` for all), optional `project_id`, `tool`, `disposition` |
| `effect` | Present but not read by the engine; a block is its own scope, not an effect value |

What the two options do with a row differs:

| | B | D |
| --- | --- | --- |
| How a row matches | The engine's selector match: every key on the row that the check also has must be equal; keys the check lacks are skipped | `Decode` turns the row into a relationship with an **anchor**, an optional **narrowing**, and **constraints**; `Evaluate` applies it if the anchor is on the object's ancestor chain |
| Row anchored too broadly | Still matches; the decision carries `legacy_broad_grant` | Matches only if the level is in `LegacyAnchoredAt`; marked legacy in the trace. Otherwise decoding fails, and the check fails |

How D reads anchors (`relschema/decode.go:80`):

| Stored selector | Anchor | Covers |
| --- | --- | --- |
| `{resource_kind: mcp, resource_id: S1}` | `mcp_server:S1` | that one server |
| `{resource_kind: mcp, resource_id: *, project_id: PA}` | `project:PA` | every server in PA |
| `{resource_kind: mcp, resource_id: *}` | the organization the grants were loaded for | every server in that organization |

`{resource_id: S1, project_id: PA}` anchors at S1 and adds a narrowing, "S1 must be in PA" (`decode.go:175`, checked at `eval.go:205`).

B reads the same selectors through `anchoredWithin` (`authz/catalog/permissions.go:87`) only to label the result: a concrete `resource_id` is a resource anchor, `*` with a `project_id` is a project anchor, `*` alone is an organization anchor.

### Fixture used in this document

Both branches test the same scenarios with their own ids. This document uses one naming:

| Here | B's tests | D's tests |
| --- | --- | --- |
| Organization `A` | `org-1` | `A` |
| Project `PA` | `proj-a` | `PA` |
| Another project `PO`, with server `SO` | `proj-2` | `PO`, `SO` |
| Server `S1` ("server A") | `srv-a` | `S1` |
| Server `S2` | (not used) | `S2` |
| Server `C` | `srv-c` | `C` |
| User `U` | `u-1` | `U` |
| Role principal | `role:organization:role-a` | `role:a` |

Every outcome in the Cases tables below is asserted in `authz/catalog/scenarios_test.go` (B) and `declarative/guide_scenarios_test.go`, `TestGuideScenarios` (D). D's walkthroughs assume organization A has the schema switched on.

---

## 5. The five MCP scenarios

| # | Scenario | B | D |
| --- | --- | --- | --- |
| 1 | Role A connects to every server in project PA | Implemented. Allowed, labelled `legacy_broad_grant` | Implemented. Allowed, labelled legacy |
| 2 | Role A manages one server, which also grants view | Implemented for the scope-settings endpoints | Implemented |
| 3 | Role B blocks server C; a user in roles A and B connects to all but C | Implemented | Implemented |
| 4 | Role C connects to every server, read-only tools only | Enforced by the engine, outside the catalogue. Gap for unannotated tools | Connect implemented. Tool check implemented in the evaluator, request sketched. Same gap |
| 5 | Role D connects to server S1, only tool A | Enforced by the engine, outside the catalogue | Connect implemented. Tool check implemented in the evaluator, request sketched |

---

### Scenario 1. Role A connects to every server in project PA

U is assigned role A.

#### Grants (same rows for B and D)

| organization_id | principal_urn | scope | selectors | effect |
| --- | --- | --- | --- | --- |
| A | role A | `mcp:connect` | `{"resource_kind": "mcp", "resource_id": "*", "project_id": "PA"}` | NULL |

#### Request

**B** (`mcp/serveendpoint.go:1056`, `:1077`, `:1092`):

```go
target := catalog.MCPServerTarget(organizationID, projectID.String(), mcpServer.ID.String())
requests, err := catalog.MCPServerUse(target, catalog.MCPServerVisibility(mcpServer.Visibility))
decision, err := catalog.NewEngineClient(s.authz).Require(ctx, requests...)
```

**D** (`mcp/serveendpoint.go:1068`, `requireProxyBackendAccess`):

```go
server := declarative.MCPServerRef{ID: mcpServer.ID.String(), ProjectID: projectID.String(), OrganizationID: organizationID}
if err := declarative.RequireMCPServer(ctx, s.authz, declarative.MCPServerConnect, server); err != nil {
	return ctx, fmt.Errorf("authorize MCP server access: %w", mcpaccess.ServerPermissionDenied(err, s.requestAccessURL(ctx, mcpServer.ID.String(), serverName)))
}
```

#### Rule setup

| B | D |
| --- | --- |
| `mcp:connect`: `AllowedAnchors: [mcp]`, `ImpliedBy: nil`, `ExcludedBy: mcp:blocked_connect`, `BlockWins: true` (`permissions.go:58`). A private server's use is one `mcp:connect` request (`mcpserver.go:55`). The engine's table: `ScopeMCPConnect: nil` (`authz/scopes.go:288`) | `connector`: `s.StoredAs("mcp:connect"), s.AnchoredAt(server), s.LegacyAnchoredAt(project, org)` (`mcp_server_schema.go:69`). `connect = (connector ∪ root) − connect_blocked` (`:80`) |

#### Walkthrough: U connects to S1

| Step | B | D |
| --- | --- | --- |
| Load | `PrepareContext` resolves U to their user principals and role A, and loads the row | Same, and records organization A as the policy's organization |
| Build | Target `mcp:S1` with ancestors `project:PA`, `org:A`; `MCPServerUse` returns one `mcp:connect` request | `MCPServerRef{S1, PA, A}`; schema switch is on for A, so `PolicySet` is fetched |
| Validate | `prepare` (`client.go:162`): definition exists, target concrete, org matches, ancestors present; builds `{mcp:connect, resource_id: S1, project_id: PA}` | `decideMCPServer` (`:144`): reference complete, policy org set; object `mcp_server:S1` → `project:PA` → `organization:A` |
| Interpret the row | Engine `evaluateGrantCheck` (`authz/grants.go:402`): `expand()` gives `root`, `mcp:connect`; selector match: kind = mcp, `*` matches S1, `project_id` PA = PA → **match** | `Decode`: `mcp:connect` → `connector`; `*` + `project_id: PA` → anchor `project:PA`; not in `Anchors`, but in `LegacyAnchors` → relationship marked legacy (`decode.go:161`, `:162`) |
| Block check | No `mcp:blocked_connect` row (`authz/checks.go:136`) → **allowed** | `Evaluate`: `project:PA` is in S1's reach (`eval.go:202`); `connect_blocked` has no rows → **allowed** |
| Label | `decide` → `anchoredWithin`: project anchor not in `AllowedAnchors` → basis `legacy_broad_grant` (`client.go:244`); the proxy logs it (`serveendpoint.go:1100`) | Trace: `project:PA#mcp_server.connector (legacy broad grant) from role:a` |
| Result | Proxy contacts the upstream | `RecordDecision` logs an allow and returns nil |

#### Cases

| Request | B | D |
| --- | --- | --- |
| connect S1, S2, C | allowed, `legacy_broad_grant` | allowed, trace says legacy |
| connect SO (project PO) | denied, `no_grant`: `project_id` PA ≠ PO | denied: `project:PA` isn't an ancestor of SO |
| same row without `project_id` | (not asserted) | allowed for every server in A, legacy, anchored at `organization:A` |
| organization not switched on | n/a: B has no per-org switch | today's rules: allowed (`mcp_server_access.go:65`) |

**To make project-wide connect first-class (sketched in both):** B changes `AllowedAnchors` to `{mcp, project}`. D changes `:69` to `s.AnchoredAt(server, project), s.LegacyAnchoredAt(org)`. Both are one-line edits; both RFC documents say connect should name one server.

---

### Scenario 2. Role A manages one server, which also grants view

U is assigned role A.

#### Grants (same rows for B and D)

| organization_id | principal_urn | scope | selectors | effect |
| --- | --- | --- | --- | --- |
| A | role A | `mcp:write` | `{"resource_kind": "mcp", "resource_id": "S1", "project_id": "PA"}` | NULL |

#### Request

**B**: the remote server's scope settings (`remotemcp/serverscopes.go:41`, enforced at `:199`). View asks for `MCPServerRead`; manage (changing the scope pin, `:63`, and again at `:235` for every server sharing the upstream URL) asks for `MCPScopePin`, which is `mcp:write` on the server.

```go
s.authorizeServerScopes(ctx, payload.McpServerID, func(target catalog.Target) []catalog.Request {
	return []catalog.Request{catalog.MCPServerRead(target)}
})
// manage:
s.authorizeServerScopes(ctx, payload.McpServerID, func(target catalog.Target) []catalog.Request {
	return catalog.MCPScopePin(target, nil)
})
```

**D**: the MCP servers service. Edit loads the row with `(id, project_id)` (`mcpservers/impl.go:413`); view is `GetMcpServer` (`:514`); `serverRef` (`:441`) builds the reference from the loaded row.

```go
// edit
declarative.RequireMCPServer(ctx, s.authz, declarative.MCPServerEdit, serverRef(organizationID, server))
// view
declarative.RequireMCPServer(ctx, s.authz, declarative.MCPServerRead, serverRef(authCtx.ActiveOrganizationID, server))
```

#### Rule setup

| B | D |
| --- | --- |
| `mcp:read`: `ImpliedBy: [mcp:write]` (`permissions.go:40`). `mcp:write`: `ImpliedBy: nil` (`:49`). Engine: `ScopeMCPRead: {ScopeMCPWrite}`, `ScopeMCPConnect: nil` (`authz/scopes.go:284`) | `editor`: `s.StoredAs("mcp:write"), anywhere, s.NarrowableBy(project)` (`:65`). `read = (viewer ∪ editor ∪ root) − read_blocked`; `edit = (editor ∪ root) − edit_blocked`; `connect` has no `editor` (`:77–80`) |

#### Walkthrough: U views S1

| Step | B | D |
| --- | --- | --- |
| Build | `authorizeServerScopes` (`serverscopes.go:189`) builds the target from the session's org and project (`serverTarget`, `:205`) | `serverRef` from the loaded row; object `mcp_server:S1` → `project:PA` → `organization:A` |
| Validate / interpret | `prepare` passes; check `{mcp:read, resource_id: S1, project_id: PA}`. `expand()` gives `root`, `mcp:read`, **`mcp:write`** | `Decode`: `mcp:write` → `editor`; concrete id → anchor `mcp_server:S1` (`decode.go:140`); leftover `project_id: PA` → `Narrowing{project: PA}` (`:175`) |
| Match | The `mcp:write` row's scope is in the expanded list; S1 = S1, PA = PA → match. No `mcp:blocked_read` → **allowed** | `read`: `viewer` false; `editor` anchor in reach (`eval.go:202`), narrowing holds (`:217`) → true; `read_blocked` false → **allowed** |
| Label | Grant names one server, in `AllowedAnchors` → no basis | Not legacy |

**U connects to S1.** B: `expand()` for `mcp:connect` gives only `root` and `mcp:connect`, so the `mcp:write` row doesn't match → `no_grant`, 403 with a request-access link (`serveendpoint.go:1098`). D: `connect` is `connector ∪ root`; no connector rows, no root → denied.

#### Cases

| Request | B | D |
| --- | --- | --- |
| view S1 | allowed: `mcp:read` expands to `mcp:write` | allowed: `read` includes `editor` |
| manage S1 (B: pin; D: edit) | allowed: direct scope match | allowed: editor anchored at S1, narrowing holds |
| connect S1 | denied, `no_grant` | denied |
| view or manage another server (B: C; D: S2) | denied, `no_grant`: `resource_id` mismatch | denied: anchor is S1 only |
| same row with `project_id: PO` | (not asserted) | everything on S1 denied: narrowing fails |

**Differs from today's production.** Today `mcp:write` implies `mcp:connect` (the engine's `scopeExpansions`), so "connect S1" is allowed. B removes that edge engine-wide on its branch (the 2026-10-29 decision for B slice 1). D removes it only for organizations with the schema switched on.

**Coverage note for B.** Other management endpoints in `remotemcp/impl.go` (e.g. `:130`, `:173`) still call the engine directly, checking `mcp:write` against the project rather than the server, so a grant on one server doesn't reach them yet.

---

### Scenario 3. Role B blocks server C; a user in roles A and B connects to all but C

U is assigned roles A and B.

#### Grants (same rows for B and D)

| organization_id | principal_urn | scope | selectors | effect |
| --- | --- | --- | --- | --- |
| A | role A | `mcp:connect` | `{"resource_kind": "mcp", "resource_id": "*", "project_id": "PA"}` | NULL |
| A | role B | `mcp:blocked_connect` | `{"resource_kind": "mcp", "resource_id": "C"}` | NULL |

A block is stored as its own scope, not through `effect`. The engine's grant query (`access/queries.sql:13`) doesn't select `effect`.

#### Request

The same call as scenario 1 in both options.

#### Rule setup

| B | D |
| --- | --- |
| `ExcludedBy: mcp:blocked_connect`, `BlockWins: true` (`permissions.go:58`). Engine: `ScopeMCPConnect: ScopeMCPBlockedConnect` (`authz/scopes.go:342`); `blockWinsScopes = [mcp:read, mcp:write, mcp:connect]` (`authz/precedence.go:51`). A block matches strictly: every key on the block row must be in the check and equal (`authz/checks.go:147`) | `s.Block("connect_blocked", s.StoredAs("mcp:blocked_connect"), anywhere, s.NarrowableBy(project), toolLimited)` (`:75`). `connect` has `s.ExcludedBy("connect_blocked")` (`:80`). Nothing in the declaration or evaluator looks at which principal holds a row |

#### Walkthrough: U connects to C

| Step | B | D |
| --- | --- | --- |
| Load | Both rows load (U holds both roles). Check `{mcp:connect, resource_id: C, project_id: PA}` | Both rows load into U's one policy |
| Allow side | `matchingGrant` finds role A's row (`*` in PA) | Row 1 → `connector` at `project:PA`, legacy → true |
| Block side | `expressionForCheck` (`checks.go:136`) builds "allow minus block", evaluated in `evaluateGrantCheckDifference` (`authz/expressions.go:278`). Role B's row matched strictly: kind = mcp, `resource_id` C = C → **block matches** | Row 2 → `connect_blocked` anchored at `mcp_server:C`. At `MinusExpr` (`eval.go:168`), the block's anchor is in C's reach; `constraintsHold` (`:232`) has nothing to check → true |
| Override? | `directOverride` (`precedence.go:109`): `ExclusionYieldsToDirectGrants(mcp:connect)` is false because connect is in `blockWinsScopes` → block stands | `sub == triTrue` (`:180`) withdraws the allow; no override path exists |
| Result | `ReasonBlocked`, 403; upstream never contacted | Denied, "withdrawn by a matching block"; `denyReason` maps to the challenge log's deny-grant reason |

For S1, the block row names C, so it doesn't match S1's check (B) or isn't in S1's reach (D), and the request is allowed.

#### Cases

| Request | B | D |
| --- | --- | --- |
| connect S1 (and S2) | allowed | allowed |
| connect C | denied, `blocked` | denied |
| call a tool on S1 | allowed | (tool path sketched; see scenario 4) |
| call a tool on C | denied: the tool-call check also carries `resource_id: C` | (tool path sketched) |
| block row held by U directly instead of role B | (not asserted) | same results: the principal isn't consulted |
| U also holds a direct `mcp:connect {resource_id: C}` | still denied: `BlockWins` | still denied: a block always wins |
| block row `{resource_id: *, project_id: PA}` | (not asserted) | every server in PA denied |

**Differs from today's production.** Today a direct user grant outranks a role block (rule R8), so the direct-grant case is allowed. Both options make blocks win for MCP scopes; B does so engine-wide on its branch, D only where the schema is on.

---

### Scenario 4. Role C connects to every server, read-only tools only

U is assigned role C. U connects to S1, then calls a read-only tool, a destructive tool, and a tool with no annotations.

#### Grants (same rows for B and D)

| organization_id | principal_urn | scope | selectors | effect |
| --- | --- | --- | --- | --- |
| A | role C | `mcp:connect` | `{"resource_kind": "mcp", "resource_id": "*", "disposition": "read_only"}` | NULL |

#### Request

Connecting is scenario 1's call in both. Tool calls differ.

**B (implemented, outside the catalogue):** the proxy attaches an interceptor (`remotemcp/proxymanager.go:366`) that calls the engine directly (`remotemcp/tools_call_authz_interceptor.go:90`, `:95`). `tools/list` uses the same check to hide tools (`tools_list_mcp_connect_filter_interceptor.go:111`).

```go
dispositions, err := i.resolver.Dispositions(ctx, i.mcpServerID, i.projectID)
err = i.authz.Require(ctx, authz.MCPToolCallCheck(i.mcpServerID, authz.MCPToolCallDimensions{
	Tool:        call.Params.Name,
	Disposition: dispositions[call.Params.Name],
	ProjectID:   i.projectID,
}))
```

Sketched for B: a catalogue builder `catalog.MCPToolCall(target, tool, disposition) Request`, used by both interceptors, with `prepare` refusing a tool with no disposition as `missing_fact` where one is required.

**D (evaluator implemented, request sketched):** today's path is the same interceptor as B. The D entry point is sketched:

```go
err = declarative.RequireMCPTool(ctx, i.authz, server, declarative.ToolFacts{
	Tool:        call.Params.Name,
	Disposition: dispositions[call.Params.Name],
})
// would evaluate connect with Request{Facts: {"tool": ..., "disposition": ...}}
```

The test (`gsDecideTool` in `guide_scenarios_test.go`) calls `mcpServerSchema.Evaluate` with those facts directly. Opening the connection passes no facts (`mcp_server_access.go:183`).

#### Rule setup

| B | D |
| --- | --- |
| No catalogue entry for tools; the rule is in the engine. `tool` and `disposition` are allowed keys on `mcp` grants (`authz/selector.go:186`). `MCPToolCallCheck` adds them to the check, leaving out empty values (`authz/checks.go:12`) | `toolLimited := s.ConstrainedBy(tool, disposition)` on `connector` (`:51`, `:69–71`). Matching is `constraintsHold` (`relschema/eval.go:232`): a present fact must equal the row's value; a missing fact passes unless the row is a block or strict (a legacy API key's row) |

Both read a tool's disposition the same way: the first set hint in its annotations (`conv/tools.go:394`), from stored annotations that a project's tool variations can override (`mcpservers/queries.sql:414`, cached in `mcpservers/tooldisposition/cache.go:77`). No annotations means disposition `""`.

#### Walkthrough

| Call | B | D |
| --- | --- | --- |
| connect S1 | The check has no `disposition` key; a grant key the check lacks is skipped (`selector.go:74`) → allowed, `legacy_broad_grant` (`*` without project) | `Decode`: `*` without `project_id` → anchor `organization:A`, a legacy level; `disposition` → `Constraints{disposition: read_only}` (`decode.go:185`). No facts, row not strict → allowed, legacy |
| `search` (read_only) | Check carries `disposition: read_only` → equal → allowed | `Facts{disposition: read_only}` → equal → allowed |
| `delete_all` (destructive) | read_only ≠ destructive → no match → `ToolPermissionDenied` | `want` ≠ `got` (`eval.go:236`) → row doesn't apply → denied |
| unannotated tool | Disposition `""` is left out of the check (`checks.go:17`); grant key skipped → **allowed** | No `disposition` fact → missing-fact branch → **allowed** |

#### Cases

| Request | B | D |
| --- | --- | --- |
| connect any server in A | allowed, `legacy_broad_grant` | allowed, legacy organization-wide row |
| call a `read_only` tool | allowed | allowed |
| call a `destructive` or `idempotent` tool | denied | denied |
| call a tool with no annotations | **allowed (gap)** | **allowed (gap, shared with today)** |
| same row on a legacy API key | (not asserted) | denied: strict rows need every fact (`eval.go:241`) |

B notes that the engine already matches tool calls strictly in one place, when a direct grant tries to outrank a block (`authz/precedence.go:138`), but not for ordinary allows.

---

### Scenario 5. Role D connects to server S1 and uses only tool A

U is assigned role D.

#### Grants (same rows for B and D)

| organization_id | principal_urn | scope | selectors | effect |
| --- | --- | --- | --- | --- |
| A | role D | `mcp:connect` | `{"resource_kind": "mcp", "resource_id": "S1", "tool": "tool_a"}` | NULL |

#### Request

Connect is scenario 1's call. Tool calls and `tools/list` are scenario 4's: the engine interceptor in B, the sketched `RequireMCPTool` in D.

#### Rule setup

| B | D |
| --- | --- |
| Same engine rules as scenario 4: `tool` is an allowed key on `mcp` grants (`selector.go:186`); `MCPToolCallCheck` puts the tool name in the check (`checks.go:14`). Names compare exactly; no patterns | Same declaration as scenario 4: `connector` with `s.AnchoredAt(server)` and `toolLimited`, matched by `constraintsHold` (`eval.go:232`) |

#### Walkthrough

| Call | B | D |
| --- | --- | --- |
| connect S1 | Check `{mcp:connect, resource_id: S1, project_id: PA}` has no `tool` key → skipped; `resource_id` matches → allowed. Names one server → no legacy basis | Concrete id → anchor `mcp_server:S1`, first-class (`decode.go:161`); `tool` → `Constraints{tool: tool_a}`; no fact, not strict → allowed |
| `tools/list` | Filter interceptor keeps only matching tools (`tools_list_mcp_connect_filter_interceptor.go:125`) → U sees only `tool_a` | (sketched, not shown) |
| `tool_b` on S1 | Check carries `tool: tool_b`; tool_a ≠ tool_b → no match → refused | `Facts{tool: tool_b}`; `want` ≠ `got` (`eval.go:236`) → denied |
| connect S2 / C | `resource_id` mismatch → denied | `mcp_server:S1` not in reach (`eval.go:202`) → denied |

#### Cases

| Request | B | D |
| --- | --- | --- |
| connect S1 | allowed | allowed |
| connect another server (B: C; D: S2) | denied | denied |
| call `tool_a` on S1 | allowed | allowed |
| call `tool_b` on S1 | denied; hidden from `tools/list` | denied |
| call `tool_a` on C | denied: `resource_id` mismatch | (not asserted) |
| plus role B `mcp:blocked_connect {resource_id: S1, tool: tool_a}`, connect S1 | (not asserted) | allowed: a tool-limited block never withdraws the whole server |
| same, call `tool_a` | (not asserted) | denied: the block matches |

---

## 6. More scenarios: projects, agents, time-bound access

These cases go beyond the five MCP scenarios. The status for each option is marked separately, because some of the code only exists on parked work and some exists nowhere yet.

| # | Scenario | B | D |
| --- | --- | --- | --- |
| 6 | Role A reads every project and writes project PA | Implemented on a parked commit | Implemented in a parked patch |
| 7 | Role B blocks project PO, but U also has a direct grant on it | Implemented on a parked commit. **The direct grant wins** | Implemented in a parked patch. **The block wins** |
| 8 | An agent connects to a server within its owner's authority | Works through the unchanged engine. Not in B's scenario tests | Works through `evaluatePolicies`. Not in D's scenario tests |
| 9 | An agent's history follows it when it is transferred | Sketched (slice 4 plan, paused). Today's rule is inline in the chat service | **Implemented** as the evidence gate at `93da99dc4`, including listing. Off in production |
| 10 | An admin gives a user access for a limited time | **Unsupported.** Sketched | **Unsupported.** Sketched |
| 11 | A request for a resource in another organization | Implemented | Implemented |

**Where the parked project work lives.** B's project step is commit [`1d6d00132`](https://github.com/speakeasy-api/gram/commit/1d6d00132741189e9050255c80c73f246e797fa0). It was reverted at `c7b47a9a3`, so it is not on the head of B's branch. D's project step was never committed. It is saved as `/mnt/project-files/rbac-option-d/step3-project-parked.patch`, on top of `0bd5c2953`. The line references below are for that commit and that patch.

**Why scenario 11 is included.** It wasn't on your list. I added it because both options promise to refuse another tenant's resource before any grant can match. That promise is easy to state, and the two options keep it in different places. The other gaps I found are not scenarios with an outcome yet. They are listed in [section 8](#8-shared-gaps-and-open-questions).

### Fixture additions

| Here | B's tests | D's tests |
| --- | --- | --- |
| Projects `PA` and `PO` in organization A | `proj-a`, and `proj-c` for the block | `PA`, `PO` |
| A third project in A, `PX` | `proj-b` | (not used) |
| Project `PB` in organization B | (not used; B uses "a project in another organization") | `PB` |
| Agent `G`, owned by U | (sketch only) | (sketch only) |

---

### Scenario 6. Role A reads every project and writes project PA

**Status.** B: implemented through the catalogue on parked commit `1d6d00132`. `GetProject` and `UpdateProject` ask through the client. D: implemented in the parked patch, with `project` grants, blocks and permissions and a `RequireProject` entry point. No service calls it yet, so the request is **sketched**. In both, about 200 (B's count) or 80 (D's count, `project:read`/`project:write` only) other call sites still use the engine directly.

#### Grants (same rows for B and D)

Case 6a, read every project:

| organization_id | principal_urn | scope | selectors | effect |
| --- | --- | --- | --- | --- |
| A | role A | `project:read` | `{"resource_kind": "project", "resource_id": "*"}` | NULL |

Case 6b, write one project:

| organization_id | principal_urn | scope | selectors | effect |
| --- | --- | --- | --- | --- |
| A | role A | `project:write` | `{"resource_kind": "project", "resource_id": "PA"}` | NULL |

#### Request

**B** (implemented, `projects/impl.go:113` and `:209`). The client is a field, built in the constructor (`:76`).

```go
// GetProject: load by slug within the caller's org, then check the loaded row
proj, err := s.repo.GetProjectBySlug(ctx, repo.GetProjectBySlugParams{Slug: slug, OrganizationID: authCtx.ActiveOrganizationID})
if _, err := s.authzClient.Require(ctx, catalog.ProjectRead(catalog.ProjectTarget(proj.OrganizationID, proj.ID.String()))); err != nil {
	// a 403 is returned as 404, so callers cannot probe for project slugs
}

// UpdateProject: the project comes from the session, not the request body
s.authzClient.Require(ctx, catalog.ProjectWrite(catalog.ProjectTarget(authCtx.ActiveOrganizationID, authCtx.ProjectID.String())))
```

**D** (sketched at the call site; `RequireProject` is implemented, `project_access.go:35`):

```go
project := declarative.ProjectRef{ID: proj.ID.String(), OrganizationID: proj.OrganizationID}
if err := declarative.RequireProject(ctx, s.authz, declarative.ProjectRead, project); err != nil {
```

#### Rule setup

**B** (`authz/catalog/permissions.go:76`, `:85`):

```go
{
	Name:              authz.ScopeProjectRead,
	Resource:          authz.ResourceKindProject,
	AllowedAnchors:    []string{authz.ResourceKindProject, authz.ResourceKindOrg}, // one project, or every project
	RequiredAncestors: projectAncestors,                                           // {org}
	ImpliedBy:         []authz.Scope{authz.ScopeProjectWrite},
	ExcludedBy:        authz.ScopeProjectBlockedRead,
	BlockWins:         false,
},
{
	Name:              authz.ScopeProjectWrite,
	Resource:          authz.ResourceKindProject,
	AllowedAnchors:    []string{authz.ResourceKindProject, authz.ResourceKindOrg},
	RequiredAncestors: projectAncestors,
	ImpliedBy:         nil,
	ExcludedBy:        authz.ScopeProjectBlockedWrite,
	BlockWins:         false,
},
```

**D** (`mcp_server_schema.go:75–85` in the patch):

```go
s.NewType(project, s.Parent(org), s.StoredKind(authz.ResourceKindProject),
	s.Grant("viewer", s.StoredAs(string(authz.ScopeProjectRead)), s.AnchoredAt(project, org)),
	s.Grant("editor", s.StoredAs(string(authz.ScopeProjectWrite)), s.AnchoredAt(project, org)),
	s.Block("read_blocked", s.StoredAs(string(authz.ScopeProjectBlockedRead)), s.AnchoredAt(project, org)),
	s.Block("write_blocked", s.StoredAs(string(authz.ScopeProjectBlockedWrite)), s.AnchoredAt(project, org)),

	s.Permission("read", s.Union(s.Rel("viewer"), s.Rel("editor"), root), s.ExcludedBy("read_blocked")),
	s.Permission("write", s.Union(s.Rel("editor"), root), s.ExcludedBy("write_blocked"), s.ExcludedBy("read_blocked")),
),
```

| Rule | B | D |
| --- | --- | --- |
| Write includes read | `project:read` `ImpliedBy: [project:write]` | `read` includes `editor` |
| A grant may name one project or every project | `AllowedAnchors: [project, org]` | `s.AnchoredAt(project, org)` |
| A read block also blocks write | Only in the engine's table (`authz/scopes.go:283`). The catalogue can't express it, because `ExcludedBy` names one block | `write` lists both `write_blocked` and `read_blocked` |
| A project grant can't be narrowed | The engine accepts only `resource_kind` and `resource_id` on project grants (`authz/selector.go:182`) | A project row with any other key fails to decode |

#### Walkthrough: U reads PA with row 6a

| Step | B | D |
| --- | --- | --- |
| Load | `GetProject` loads PA by slug within A. An unknown slug, or one in another organization, is a 404 before any check | Same handler, today's load |
| Validate | `prepare`: definition exists, target concrete, org = active org, ancestor org present. Check `{project:read, resource_id: PA}`, with no `project_id` | `RequireProject`: switch on, `PolicySet` fetched, `decideProject` builds `project:PA` → `organization:A` |
| Interpret | `expand()` gives `root`, `project:read`, `project:write`. Role A's row: scope in list, `*` matches → match | `Decode`: `project:read` → `viewer`; `*` with no ancestor key → anchor `organization:A` (`decode.go:144`); an allowed anchor, not legacy |
| Decide | No `project:blocked_read` → allowed. `*` with no `project_id` is an org anchor, which is allowed → no legacy basis | `organization:A` is in PA's reach; `read_blocked` has no rows → allowed |

Listing projects is a different path. In B, `ListProjects` still calls `s.authz.Filter` (`projects/impl.go:280`), because the catalogue has no filter operation. In D, listing isn't covered.

#### Cases

| Request | B | D |
| --- | --- | --- |
| 6a: read PA, read PO | allowed | allowed |
| 6a: write PA | denied, `no_grant` | denied |
| 6a: read a project in organization B | denied, `cross_tenant`, in `prepare` | denied, "object belongs to another organization" |
| 6b: write PA | allowed | allowed |
| 6b: read PA | allowed: `project:read` expands to `project:write` | allowed: `read` includes `editor` |
| 6b: read or write another project | denied, `no_grant` | denied |
| only `mcp:write {mcp, *, project_id: PA}`: read PA | (not asserted) | denied: a grant on the servers in a project isn't a grant on the project |

Both options keep today's outcomes for 6a and 6b.

---

### Scenario 7. Role B blocks project PO, but U also has a direct grant on it

**Status.** The same as scenario 6. This is the one project case where B and D give **different answers**.

#### Grants (same rows for B and D)

U holds roles A and B. Case 7a has a write block, case 7b a read block. The last row is the variation, a grant made directly to U.

| organization_id | principal_urn | scope | selectors | effect |
| --- | --- | --- | --- | --- |
| A | role A | `project:write` | `{"resource_kind": "project", "resource_id": "*"}` | NULL |
| A | role B | 7a: `project:blocked_write`, 7b: `project:blocked_read` | `{"resource_kind": "project", "resource_id": "PO"}` | NULL |
| A | `user:U` (variation) | `project:write` | `{"resource_kind": "project", "resource_id": "PO"}` | NULL |

#### Request

`UpdateProject` and `GetProject`, as in scenario 6.

#### Rule setup

| B | D |
| --- | --- |
| `project:write` has `ExcludedBy: project:blocked_write`, **`BlockWins: false`**. Projects aren't in the engine's `blockWinsScopes` (`authz/precedence.go:51`), so a direct user grant on one resource outranks a role's block. That is today's rule, R8 | `write` is "allows, minus `write_blocked`, minus `read_blocked`". The evaluator never looks at which principal holds a row, so **a block always wins** |

#### Walkthrough: U writes PO, with the 7a rows and the direct grant

| Step | B | D |
| --- | --- | --- |
| Allow side | Role A's `*` row matches | Role A's row → `editor` at `organization:A` → true |
| Block side | The exclusion (`authz/checks.go:136`) matches role B's `project:blocked_write` strictly on `resource_id` PO | Role B's row → `write_blocked` at `project:PO` → true; the allow is withdrawn (`eval.go:180`) |
| Override | `directOverride` (`precedence.go:109`): `ExclusionYieldsToDirectGrants(project:write)` is **true**. `user:U`'s row names PO → **it outranks the block** | No override step exists |
| Result | **allowed** | **denied**, "withdrawn by a matching block" |

#### Cases

| Request | Today | B | D |
| --- | --- | --- | --- |
| 7a: write PA | allowed | allowed | allowed |
| 7a: write PO, no direct grant | denied | denied, `blocked` | denied |
| 7a: read PO | allowed | allowed: a write block doesn't withdraw read | allowed: `read` doesn't name `write_blocked` |
| 7a: write PO, with U's direct grant | **allowed** (R8) | **allowed** | **denied** |
| 7b: read PO | denied | denied, `blocked` | denied |
| 7b: write PO | denied | denied: a read block also counts as a write block (engine table) | denied: `write` excludes `read_blocked` |

**The difference is a choice, not a limit of either option.** B kept today's project rules and could switch them with `BlockWins: true`, plus adding projects to the engine's `blockWinsScopes`. D would need a precedence concept to keep R8: something like `s.ExcludedBy("write_blocked", s.YieldsToDirectGrant())`, with the evaluator told which rows are direct user grants. That is **sketched, not built**. In scenario 3, by contrast, both options make the MCP block win.

---

### Scenario 8. An agent connects to a server within its owner's authority

**Status.** This works through code that exists today, in both options, but neither option's scenario tests assert it. B: the catalogue hands checks to the engine, which already evaluates an agent's policies. D: `PolicySet` returns the same policies, and `evaluatePolicies` (`mcp_server_access.go:184`) allows only if every one allows. Both rows below are therefore **follows from implemented code, not asserted**.

#### How an agent's authority is built today

An agent session doesn't run with the agent's grants alone. Admission (`agents/runtimepolicy/workload_admission.go`, and `credential_admission.go` for principal credentials) loads three policies, and every check must pass **all three**:

| Policy | Where it comes from | Live or fixed |
| --- | --- | --- |
| R, the ceiling | The delegated grants stored on the session's credential at issuance (`contextvalues.PrincipalCredential.DelegatedGrants`) | Fixed |
| A, the agent's policy | `principal_grants` rows for `agent:<id>` and for every role in `agent_role_assignments` (`runtimepolicy/load.go:22`) | Live |
| O, the owner's policy | The current owner's own effective grants (`agents.owner_user_id`, `ResolveEligibleUser`) | Live |

Admission refuses outright if the agent is suspended, revoked or deleted, if its owner needs reassignment, or if the owner isn't eligible. The engine loops over the policies (`authz/context.go:188`) and allows only if every policy has a matching grant.

#### Data (same for B and D)

`agents`:

| id | organization_id | owner_user_id | project_id | suspended_at | revoked_at | owner_reassignment_required_at |
| --- | --- | --- | --- | --- | --- | --- |
| G | A | U | PA | NULL | NULL | NULL |

`agent_role_assignments`:

| organization_id | agent_id | role_urn |
| --- | --- | --- |
| A | G | role E |

`principal_grants`:

| organization_id | principal_urn | scope | selectors | effect | feeds policy |
| --- | --- | --- | --- | --- | --- |
| A | `agent:G` | `mcp:connect` | `{"resource_kind": "mcp", "resource_id": "S1"}` | NULL | A |
| A | role E | `mcp:connect` | `{"resource_kind": "mcp", "resource_id": "S2"}` | NULL | A |
| A | role A (held by U) | `mcp:connect` | `{"resource_kind": "mcp", "resource_id": "*", "project_id": "PA"}` | NULL | O |

The ceiling R is stored on the credential, not in `principal_grants`. Here it covers `mcp:connect` on every server in PA.

#### Request

The same proxy call as scenario 1, in both options. Nothing at the call site changes for an agent. The difference is in what `PrepareContext` loaded: `AdmitWorkloadSession` or `AdmitPrincipalCredential` (`authz/engine.go:112`) puts R, A and O on the context instead of one user policy.

#### Rule setup

No new rule in either option. Each evaluates its normal `mcp:connect` rule once per policy:

| B | D |
| --- | --- |
| The catalogue validates the request once, then the engine evaluates the check against each policy (`context.go:197`). An allow needs a match in R, A and O | `decideMCPServer` decodes and evaluates each policy separately. `PolicySet.Policies` is documented as "every one must allow" (`authz/policy_set.go`) |

#### Walkthrough: agent G connects to S1

| Step | B | D |
| --- | --- | --- |
| Admit | `AdmitWorkloadSession` checks G is active and U is eligible, then loads R, A, O | Same |
| Build | Target `mcp:S1` in PA, A; `prepare` passes | `MCPServerRef{S1, PA, A}`; `PolicySet` has three policies bound to A |
| R | Matches `mcp:connect` in PA | `connector` at `project:PA` (legacy) → true |
| A | `agent:G`'s row names S1 → match | `connector` at `mcp_server:S1` → true |
| O | Role A's `*` in PA → match | `connector` at `project:PA` (legacy) → true |
| Result | allowed | allowed |

#### Cases

| Request | B | D | Why |
| --- | --- | --- | --- |
| G connects to S1 | allowed | allowed | R, A and O all allow |
| G connects to S2 | allowed | allowed | A allows through role E |
| G connects to C | denied | denied | A has no row for C |
| U loses role A, then G connects to S1 | denied | denied | O is loaded live and no longer allows |
| G is transferred to an owner with no connect grant | denied | denied | O is the new owner's policy |
| role B blocks C for U, and A gains `agent:G mcp:connect C` | denied | denied | The block is in O. MCP blocks always win, and agent grants never override a block today (`precedence.go:31`) |
| G is suspended | refused at admission | refused at admission | No policy reaches either option |

**One difference to check.** The engine also binds `$represented_user` in selectors and keeps the represented user's own policies for auth-client checks (`authz/context.go:25`). D's `PolicySet` exposes only `Policies` and `OrganizationID`, not those two. MCP connect checks don't use them, but a row with a `$represented_user` selector would probably fail to decode in D. That is **unverified**.

---

### Scenario 9. An agent's history follows it when it is transferred

> **Update (2026-10-30, D at [`93da99dc4`](https://github.com/speakeasy-api/gram/commit/93da99dc4)).** D built this scenario as the evidence gate the external review asked for. The D column below is the original sketch. The built version differs in these ways:
>
> - **Language added to `relschema`:** `s.Attr` with `Object.Attrs`, `s.Is(attr)`, `s.Via(attr, perm)` with `Request.Objects`, `s.Unless(attr, expr)` (replacing the sketched `SoleAttribution`), and `Request.Subject`. No escape hatches were needed in the evaluator.
> - **Rule** (`chat_schema.go:43–66`): `read = Is(author) ∪ Unless(author, Unless(external_author, Via(agent, owner))) ∪ reader ∪ writer ∪ root`. `agent.owner = Is(owner)`. All `agent:*` management scopes are declared out of schema, so management never reaches transcripts.
> - **Wiring:** the switch sits in the shared gate `authorizeChatAccess` (`chat/impl.go:1074`), so `LoadChat`, `LoadChatOverview` and the conversation path change together. `chatReadFacts` loads the agent with the chat's organization; a missing row becomes "no fact", which is undetermined.
> - **Listing:** `PlanChatList` asks the schema only "every session or not?". The owner and author branches are a **hand-written SQL mirror** (`chat/queries.sql:626`, `:771`), kept equal by an agreement test across pages. Generating that SQL from the schema isn't built. This is the main cost the slice exposed.
> - **All six acceptance cases pass**, in memory (`TestAgentHistoryAcceptance`) and against Postgres through the real service (`TestAgentHistorySchema`). A mutation check confirms the DB test runs the schema path.
> - **Defaults that are D's choices, not decisions:** no capture-time marker column (activation is per organization); `chats.user_id` counts as the author, as today, though it's resolved from a hook payload email; ownership isn't capped by a second admitted policy, as today.
> - **Policy-adjacent Go outside the schema:** `chatObject` sets the owner attribute only for an eligible agent (not latched, not deleted), and `PlanChatList`.

**Status.** **Sketched in both.** This was slice 4, which is paused after Stage A. Each option has a plan: `/mnt/project-files/rbac-eval/option-b/slice4-stage-a.md` (B) and `/mnt/project-files/rbac-option-d/slice4-stage-a.md` (D). **Today's behavior is already implemented**, inline in the chat service (PR #8110). U1, the current owner of an agent, can read that agent's agent-only sessions. Ownership is resolved at read time, so a transfer moves access, and the former owner loses it at once.

#### Data (same for B and D)

`agents`, before and after transfer:

| id | organization_id | owner_user_id |
| --- | --- | --- |
| G | A | U1, then U2 |

`chats`:

| id | organization_id | project_id | agent_id | user_id | external_user_id | captured |
| --- | --- | --- | --- | --- | --- | --- |
| C1 | A | PA | G | NULL | NULL | after activation |
| C2 | A | PA | G | U1 | NULL | after activation |
| C3 | A | PA | NULL | NULL | NULL | after activation |
| C4 | A | PA | G | NULL | NULL | before activation |

`principal_grants`:

| organization_id | principal_urn | scope | selectors |
| --- | --- | --- | --- |
| A | `user:M` | `agent:write`, `agent:transfer`, `agent:read` | `{"resource_kind": "agent", "resource_id": "G"}` |
| A | `user:R` | `chat:read` | `{"resource_kind": "chat", "resource_id": "*"}` |

Both plans add a per-session marker recording which rules applied at capture (B: a nullable `chats.access_rules` column; D: the same idea, still an open question). Neither adds a grant row for ownership: **ownership is a relationship read from `agents.owner_user_id`, not a grant.**

#### Request

**Today** (`chat/impl.go:1060`, `authorizeChatAccess`):

```go
// reads only: an agent-only session is readable by the agent's current owner
if agentOnlySession && IsAgentOwner{agent_id, chat.organization_id, caller} { allow }
// otherwise: authz.Require(ChatReadCheck)
```

**B (sketched):** the chat service passes facts, and the catalogue resolves ownership through the backend.

```go
facts := catalog.SessionFacts{OrgID, ProjectID, ChatID, AuthorUserID, ExternalUserID, AgentID, Rules}
s.authzClient.Require(ctx, catalog.SessionRead(facts))
```

**D (sketched):** the chat service builds a reference and loads the owner fact itself.

```go
ref := declarative.ChatRef{ID, ProjectID, OrganizationID, AuthorUserID, ExternalUserID, AgentID, Rules}
owner := declarative.AgentOwnerFact{AgentID, OrganizationID, OwnerUserID, Eligible}
declarative.RequireChatRead(ctx, s.authz, ref, owner)
```

#### Rule setup

**B (sketched).** A `chat:read` definition with a new field, `Relations`. A relation is a **closed Go enum** whose cases are hand-written functions in the client, not a data-driven rule:

```go
{
	Name:              authz.ScopeChatRead,
	Resource:          "chat",
	AllowedAnchors:    []string{"chat", authz.ResourceKindProject, authz.ResourceKindOrg},
	RequiredAncestors: []string{authz.ResourceKindProject, authz.ResourceKindOrg},
	ImpliedBy:         []authz.Scope{authz.ScopeChatWrite},
	Relations:         []Relation{RelationSessionAuthor, RelationAgentOwner},
}
// Backend gains: agentOwnership(ctx, orgID, agentID) (Ownership{OwnerUserID, Latched, Deleted}, found)
```

**D (sketched).** New builder pieces for attributes and traversal. `SoleAttribution` stops the owner of an agent reading a session that also has a human author:

```go
s.NewType("agent", s.Parent("organization"), s.Attr("owner", "user"),
	s.Permission("owner", s.Is("owner")), // only the current owner; management grants are deliberately absent
),
s.NewType("chat", s.Parent("project"),
	s.Attr("author", "user"), s.Attr("agent", "agent"),
	s.Grant("reader", s.StoredAs("chat:read"), s.AnchoredAt("organization")),
	s.Permission("read", s.Union(
		s.Is("author"),
		s.SoleAttribution("agent", s.Via("agent", "owner")),
		s.Rel("reader"),
	)),
),
```

| | B | D |
| --- | --- | --- |
| New concept | `Relations` on a definition, with each relation hard-coded | Attributes, `Is`, `Via`, `SoleAttribution` in the schema language |
| Who loads ownership | The catalogue's backend, with a new authz-owned query bound to the organization | The chat service, as a fact passed in. A missing fact is undetermined, never allowed |
| Pre-activation sessions | Today's rule, kept as the "legacy" version | The legacy rule, moved unchanged |

#### Walkthrough: U2 reads C1 after G is transferred from U1

| Step | B (sketched) | D (sketched) |
| --- | --- | --- |
| Load | `GetChat{id, project_id}`, tenant-bound to the caller's project | Same |
| Classify | Attribution is `agent` (agent set, no human fields) | `agent` is the sole attribution |
| Author | No author → skip | `Is("author")` false |
| Owner | The backend looks up G's ownership in A: owner U2, not latched → allowed, basis "agent owner" | `Via("agent", "owner")` with the fact owner = U2 → true |
| Result | allowed | allowed |

#### Cases (from the two plans' fixtures)

| Request | Today | B | D |
| --- | --- | --- | --- |
| U1 reads C1 before transfer | allowed | allowed | allowed |
| U1 reads C1 after transfer | denied | denied | denied |
| U2 reads C1 after transfer | allowed | allowed | allowed |
| M (manages and transfers G) reads C1 | denied | denied: management isn't read | denied |
| R (`chat:read`) reads C1 | allowed | allowed, basis grant | allowed |
| U1 reads C2 (their own human session, G stamped too) after transfer | allowed | allowed: author | allowed: author |
| U2 reads C2 | denied | denied: conflicting attribution | denied: `SoleAttribution` |
| anyone but R reads C3 (no attribution) | denied | denied | denied |
| C4 (before activation) | today's rule | legacy rule, same answers | legacy rule, same answers |
| G soft-deleted, its last owner reads its session | denied | allowed for sessions captured after activation (RFC rule) | (not covered in D's plan) |

**Note.** D's plan found that the earlier spike would have let anyone with `agent:write` read the transcripts, and let an agent's owner into a session with a human author. The sketch above avoids both.

---

### Scenario 10. An admin gives a user access for a limited time

**Status.** **Unsupported in both options, and today.** `principal_grants` has no expiry column (`database/schema.sql:9374`), and nothing in either option reads a time. The sketches below are not on any branch. Two other tables do have `expires_at` (`platform_mcp_authorization_grants`, `admin_mcp_authorization_grants`), so the pattern exists in the codebase.

#### Data model (sketched, shared)

A nullable column on the existing table, written by the admin's grant call:

| organization_id | principal_urn | scope | selectors | effect | **expires_at** |
| --- | --- | --- | --- | --- | --- |
| A | `user:U` | `mcp:connect` | `{"resource_kind": "mcp", "resource_id": "S1"}` | NULL | `2026-11-06 17:00Z` |

Both options read rows through the same load (`authz/load.go:14`, `access/queries.sql:13`), so the simplest version is shared. The load leaves out expired rows: `WHERE expires_at IS NULL OR expires_at > now()`. Neither option's rule language changes. An expired row behaves exactly as a deleted one.

#### Request

Unchanged in both: scenario 1's proxy call.

#### Rule setup (sketched, if the decision should say why)

Filtering at load gives the right answer, but the decision can't say "expired" and the UI can't say "expires Friday". To report that, each option would carry the time through:

| B | D |
| --- | --- |
| `authz.Grant` gains `ExpiresAt`. `decide` adds a `Basis` value, e.g. `time_bound`, when the matching grant has one, and `Authorize` can return the expiry for the UI | `relschema.Row` gains `ExpiresAt`. `Decode` keeps it on the relationship and the trace says "until …". A per-grant option such as `s.Grant("connector", ..., s.TimeBound())` could restrict which relations an admin may grant for a limited time, and `Compile` would check it |

The same table in each option would also cover "can an admin give *this* permission for a limited time at all", if that needs to be limited.

#### Walkthrough: U connects to S1, on 2026-11-05, then 2026-11-07

| Step | Both (sketched) |
| --- | --- |
| 11-05 | The load query returns the row; scenario 1's path; allowed |
| 11-07 | The load query skips the row; no grant matches; denied, `no_grant` (B) or "no relationship grants…" (D) |

#### Cases and things to decide

| Question | Both options |
| --- | --- |
| A long-lived MCP connection opened before expiry | The connect check runs when the connection opens. Tool calls are checked per call, so they stop at expiry. The open connection itself isn't cut |
| A time-bound allow and a permanent block | The block still wins (MCP), or follows each option's project rule from scenario 7 |
| Re-granting the same row with a new expiry | The unique index is on `(organization_id, principal_urn, scope, selectors)`, so it's an update, not a second row |
| Cleaning up expired rows | Not needed for correctness if the load filters. A cleanup job would keep the table small |
| Audit | The grant and its expiry go through the existing grant-change audit. An "expired" event would be new |
| Agents | An owner's time-bound grant lapses in O (scenario 8), so their agents lose it at the same moment |

---

### Scenario 11. A request for a resource in another organization

**Status.** Implemented in both, for MCP servers: `TestClient_refusesCrossTenantTargetDespiteGrant` (B, `catalog/client_test.go:151`) and the "another organization" case in `mcp_server_access_test.go` (D). For projects: B's `prepare` applies the same check to any definition; D asserts it in the parked project tests.

#### Grants (same rows for B and D)

U is a member of organization A only:

| organization_id | principal_urn | scope | selectors | effect |
| --- | --- | --- | --- | --- |
| A | role A | `project:read` | `{"resource_kind": "project", "resource_id": "*"}` | NULL |

The request is for project PB, which belongs to organization B. It can only reach the check if a handler built the target from a row it shouldn't have loaded.

#### Request

As in scenario 6, with a reference or target whose organization is B.

#### Rule setup

| B | D |
| --- | --- |
| `prepare` (`client.go:180`): the target's organization must equal the caller's active organization, or the outcome is `cross_tenant`. Every required ancestor must be in the same organization (`:185`) | `Evaluate` (`eval.go:87`): the object's organization must equal the organization the policy was loaded for. A `*` row only ever anchors at that organization |

#### Walkthrough

| Step | B | D |
| --- | --- | --- |
| Where it stops | In `prepare`, before any grant is read | In `Evaluate`, before any relationship is applied |
| Result | denied, `cross_tenant` | denied, "object belongs to another organization" |

#### Cases

| Request | B | D |
| --- | --- | --- |
| read PB with a `*` row in A | denied, `cross_tenant` | denied |
| MCP server in B | denied, `cross_tenant` | denied |
| a server in A whose project ancestor is in B | denied, `cross_tenant` (ancestor check) | (not asserted: `MCPServerRef` carries one organization, so the case can't be built) |
| today, the same check through the engine | relies on the handler's tenant-scoped load | relies on the handler's tenant-scoped load |

Today the engine has no tenant check of its own on the target. It relies on the handler having loaded the row within the caller's organization. Both options add an explicit check, at different points.

---

## 7. Legacy broad grants

Both options honor rows that are broader than the target design and label the allow instead of refusing it. This matches the shared decision that wildcard `mcp:connect` grants are honored as legacy until migrated.

| | B | D |
| --- | --- | --- |
| Which rows | Any `mcp:connect` row whose anchor isn't one server: `*` with `project_id` (project), or `*` alone (org) | The same shapes, because `connector` lists project and org under `LegacyAnchoredAt` |
| What happens | Allowed; `Decision.Basis = legacy_broad_grant`; the proxy logs it | Allowed; the relationship is marked `Legacy` and the trace says "legacy broad grant" |
| Rows outside any declared anchor | Still allowed by the engine (only labelled) | Rejected at decode time (`decode.go:165`), which fails the check |
| Scenarios where it shows | 1, 3 (role A's row), 4 | 1, 3, 4 |
| Making it first-class | `AllowedAnchors: {mcp, project}` (sketched) | `s.AnchoredAt(server, project), s.LegacyAnchoredAt(org)` (sketched) |

Behavior that changes from today's production in both options (B engine-wide on its branch, D per organization behind the switch):

| Rule | Today | B | D |
| --- | --- | --- | --- |
| `mcp:write` implies `mcp:connect` | yes | no | no |
| A direct user grant outranks a role block (R8), MCP | yes | no | no |
| A direct user grant outranks a role block (R8), projects | yes | yes (unchanged) | no |

---

## 8. Shared gaps and open questions

**Gaps both options have today**

- **Unannotated tools under a read-only row** (scenario 4). A tool with no annotations has disposition `""`, which neither option treats as a mismatch. In B the engine skips the key; in D the missing-fact branch passes for non-strict rows. Deciding whether an unannotated tool should count as not read-only is a behavior decision, not only code. B sketches `missing_fact` in `prepare`; D could mark tool-limited rows strict.
- **Tool calls are not yet decided by the new model.** In B they stay in the engine interceptors (`MCPToolCall` builder sketched). In D the evaluator handles tool facts and is tested, but no request path calls it (`RequireMCPTool` sketched). Today, in both branches, the live tool-call path is the legacy interceptor.
- **Coverage of endpoints.** Only some MCP server endpoints go through the new path. B covers the remote server's scope settings and the private endpoint proxy; other management endpoints in `remotemcp/impl.go` still check `mcp:write` on the project. D covers the MCP servers service's read and edit and the private endpoint proxy. In both, most of the code base still calls `authz.Engine.Require` with hand-built checks.
- **Listing.** B's `ListProjects` still calls `s.authz.Filter`; the catalogue has no filter operation. D now lists sessions (`93da99dc4`), with the schema deciding "all or not" and a hand-written SQL mirror for the owner branches, held equal by a test. D doesn't list projects or MCP servers.
- **Time-bound access** (scenario 10). No expiry anywhere in the grant model. The shared sketch is a nullable `expires_at` filtered at load.
- **Agents in the new model's tests** (scenario 8). Both options inherit the three-policy rule from the engine, but neither scenario suite asserts it. D's `PolicySet` also doesn't carry the `$represented_user` binding.

**Open questions per option**

| B | D |
| --- | --- |
| The engine and the catalogue hold the same rules twice; a test keeps them equal | Activation is a callback with no implementation: feature flag or organization column is undecided |
| Only MCP server permissions are defined; each new resource adds definitions and a builder file | Entry points are per type (`RequireMCPServer`); a generic `Require(ctx, engine, permission, ref)` comes with a second type |
| One exception to the client-as-field pattern: the `mcp` service builds the client at the call because tests construct it as a struct literal | One compiled schema for all types, or one per area, is open; with one schema every row in a policy must decode for every check |
| Whether project-wide connect is first-class (scenario 1) | Same question |
| The catalogue can't express "a read block also blocks write"; it lives only in the engine's table (scenario 7) | One activation switch (`MCPServerSchemaActive`) now covers projects too; a per-type switch needs a second option, and the name is too narrow |
| Relationship rules (scenario 9) need a new `Relations` field and a backend lookup per relation | Attributes and traversal were added (`93da99dc4`); listing still needs a hand-written SQL mirror. A malformed row in a mapped scope fails every schema check for that caller (fail closed, 500); `AuditGrants` finds such rows before activation |
