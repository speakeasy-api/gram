# Option B, step by step

This guide explains option B one step at a time, in plain words, with the code
that implements each step. New steps are added at the end as they are asked
for.

Labels:

- **Implemented**: the code exists on this branch and has tests.
- **Sketched**: proposed code that is not wired in or not tested.

## Steps

1. [The scaffolding: defining checks and injecting the client](#step-1-the-scaffolding)
2. [Five role scenarios for MCP servers](#step-2-five-role-scenarios-for-mcp-servers)

---

## Step 1. The scaffolding

**Status:** implemented. Everything below is in `server/internal/authz/catalog`
on this branch.

### Where it sits

```
handler (e.g. remotemcp)
   │  builds a Target from records it loaded, asks for Requests
   ▼
catalog.Client          checks each Request against its Definition
   │                    (right kind of resource, same organization, containers present)
   ▼
catalog.Backend         one implementation today: the existing engine
   ▼
authz.Engine            unchanged: loads the caller's grants from principal_grants and matches them
```

The catalogue adds no new storage. Grants, roles and the engine stay as they
are; the catalogue decides what a check means before the engine reads any grant.

### 1. How a check is defined

Each permission the code can check is one `Definition` (`permissions.go:11`):

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

All definitions live in one list, `definitions` (`permissions.go:39`). It holds
the three MCP server permissions so far. Two safeguards keep the list honest:

- **At startup.** `mustValidate` runs when the package loads and panics on a bad
  entry: a duplicate, a block used as a permission, a resource kind that does not
  match the scope, an implication across kinds, or a missing block.
- **In tests.** The engine cannot import this package, so it keeps its own scope
  tables. `TestDefinitionsMatchEngine` (`client_test.go:69`) fails if what the
  engine implies, which block it pairs with, or whether a block wins ever
  differs from the list.

### 2. The types a handler uses

From `client.go`:

```go
// One resource and the organization that owns it.
type ResourceRef struct{ OrganizationID, Kind, ID string }

// The resource being acted on, plus the containers it actually sits in.
// Built from rows the handler loaded, never from request input alone.
type Target struct {
	Resource  ResourceRef
	Ancestors []ResourceRef
}

// "Does the caller hold Permission on Target?"
type Request struct {
	Permission authz.Scope
	Target     Target
}

// The answer.
type Decision struct {
	Outcome Outcome  // allowed, denied, or unresolved (a fact was missing; never allows)
	Reason  Reason   // allowed, no_grant, blocked, cross_tenant, missing_fact, not_enforced
	Basis   Basis    // set when an allow rested on an old, broader grant
	Request Request  // the first request that was not allowed
	Grants  []authz.Grant
}
```

Handlers do not assemble these by hand. Each resource kind has a small file of
builders, so the check for an operation is written once. For MCP servers it is
`mcpserver.go`:

```go
func MCPServerTarget(organizationID, projectID, serverID string) Target
func MCPServerRead(target Target) Request
func MCPScopePin(target Target, sharing []Target) []Request
func MCPServerUse(target Target, visibility MCPServerVisibility) ([]Request, error)
```

### 3. How the client is built

`client.go:92` and `backend.go`:

```go
type Backend interface {
	evaluate(ctx, checks) ([]authz.CheckResult, error) // decide, record nothing
	enforce(ctx, checks) error                         // decide and record the challenge
	reject(ctx, err) error                             // map a refusal to the API error
}

func NewClient(backend Backend, defs []Definition) (*Client, error)
func NewEngineClient(engine *authz.Engine) *Client // NewClient(NewEngineBackend(engine), Definitions())
```

The backend is the only part that talks to the engine. It is picked once, when
the client is built, so a different backend (a fake in tests, or a future
store) would not change any handler.

### 4. How the client reaches a handler

The engine is built once at startup (`cmd/gram/start.go:1028`) and passed to
each service constructor, as it was before. The service builds its client from
that engine and keeps it as a field (`remotemcp/impl.go:55`, `:96`):

```go
type Service struct {
	// ...
	authz       *authz.Engine
	authzClient *catalog.Client
}

func NewService(..., authzEngine *authz.Engine, ...) *Service {
	return &Service{
		// ...
		authz:       authzEngine,
		authzClient: catalog.NewEngineClient(authzEngine),
	}
}
```

So no constructor signature changes, and `start.go` does not change.

One exception: the `mcp` service builds the client at the call
(`catalog.NewEngineClient(s.authz)`, `mcp/serveendpoint.go:1092`) instead of
keeping a field. Several of its tests create `mcp.Service` as a struct literal,
which would leave the field nil and panic. Building the client is cheap, since
it only copies the definition list, but the field is the intended pattern.

### 5. The call shape

A handler asks one of two questions:

- `Require(ctx, requests...)` enforces. If anything is not allowed it returns a
  403 error, and the engine records the challenge exactly as before.
- `Authorize(ctx, requests...)` answers "could I?" for the UI, such as a
  `can_pin` flag, without recording anything. It reaches the same decision as
  `Require` for the same requests; tests check that the two agree.

From `remotemcp/serverscopes.go`:

```go
// enforce (serverscopes.go:235)
if _, err := s.authzClient.Require(ctx, scopePinRequests(authCtx, target, sharing)...); err != nil {
	return nil, err
}

// capability (serverscopes.go:244)
decision, err := s.authzClient.Authorize(ctx, scopePinRequests(authCtx, target, sharing)...)
return decision.Allowed()
```

Both lines use the same request builder, so the "can I?" answer and the
enforcement cannot drift apart.

### What the client does on every call

1. **Validate** (`client.go:162`, `prepare`). The permission must have a
   definition. The target must be one concrete resource of the right kind, not
   a wildcard. Its organization must be the caller's active organization, or
   the answer is `cross_tenant`. Every required container must be present and
   in the same organization, or the answer is `missing_fact`. All of this
   happens before any grant is read.
2. **Evaluate** through the backend, which uses the grants the engine already
   loaded for this request.
3. **Decide** (`client.go:229`). This returns allowed, or the first request
   that failed and why. An allow that came from an old grant broader than the
   definition now permits is labelled `legacy_broad_grant`.

### Not part of step 1

- Only the MCP server permissions are defined so far. Other resources would add
  entries to the same list and a builder file of their own.
- Most of the code base still calls `authz.Engine.Require` with hand-built
  checks. The scaffolding lets each operation move over on its own; it does not
  move them.

---

## Step 2. Five role scenarios for MCP servers

Each scenario follows the same template: **Grants**, **Request**, **Rule
setup**, **Walkthrough**, **Cases**. File references are `file:line` on this
branch, under `server/internal/`. Every outcome in the Cases tables is
asserted in `authz/catalog/scenarios_test.go`.

All scenarios use the same synthetic setup:

- Organization `org-1`, project `proj-a`, private remote MCP servers `srv-a`
  and `srv-c` in `proj-a`.
- User `u-1`, a member of `org-1`. When a request arrives, the engine resolves
  the user to principals `user:u-1`, `user:all` and one
  `role:organization:<role>` per assigned role (`authz/engine.go:178`), then
  loads every `principal_grants` row for those principals
  (`authz/engine.go:196`, `authz/load.go:14`).

| # | Scenario | Status |
| --- | --- | --- |
| 1 | Role A connects to every server in project A | Implemented through the catalogue. Allowed, but labelled a legacy broad grant. |
| 2 | Role A manages one server, which also grants view | Implemented through the catalogue. |
| 3 | Role B blocks server C; a user in roles A and B connects to all but C | Implemented through the catalogue. |
| 4 | Role C connects to every server, read-only tools only | Enforced by the engine, outside the catalogue. Has a gap for tools without annotations. |
| 5 | Role D connects to server A and uses only tool A | Enforced by the engine, outside the catalogue. |

---

### Scenario 1. Role A can connect to every server in project A

**Status:** implemented through the catalogue. Allowed, but labelled
`legacy_broad_grant`.

#### Grants

`principal_grants` rows that `u-1` resolves to:

| principal_urn | scope | selectors | effect |
| --- | --- | --- | --- |
| `role:organization:role-a` | `mcp:connect` | `{"resource_kind": "mcp", "resource_id": "*", "project_id": "proj-a"}` | NULL (allow) |

#### Request

The private endpoint proxy, before contacting the upstream server
(`mcp/serveendpoint.go:1056`, `:1077`, `:1092`):

```go
target := catalog.MCPServerTarget(organizationID, projectID.String(), mcpServer.ID.String())
requests, err := catalog.MCPServerUse(target, catalog.MCPServerVisibility(mcpServer.Visibility))
decision, err := catalog.NewEngineClient(s.authz).Require(ctx, requests...)
```

#### Rule setup

The definition (`authz/catalog/permissions.go:58`):

```go
{
	Name:              authz.ScopeMCPConnect,
	Resource:          authz.ResourceKindMCP,
	AllowedAnchors:    []string{authz.ResourceKindMCP}, // a grant should name one server
	RequiredAncestors: mcpServerAncestors,              // {project, org}
	ImpliedBy:         nil,
	ExcludedBy:        authz.ScopeMCPBlockedConnect,
	BlockWins:         true,
},
```

What using a server requires (`authz/catalog/mcpserver.go:55`):

```go
case MCPServerPrivate:
	return []Request{{Permission: authz.ScopeMCPConnect, Target: target}}, nil
```

The engine's matching table, kept equal to `ImpliedBy` by
`TestDefinitionsMatchEngine` (`authz/scopes.go:288`):

```go
ScopeMCPConnect: nil, // nothing else satisfies connect
```

#### Walkthrough

`u-1` connects to `srv-a`.

1. `mcp/serveendpoint.go:1013` calls `authorizeProxyBackendAccess` with
   `org-1`, taken from the server's project row (`:944`), and `proj-a`.
2. `:1056` builds the target: server `srv-a`, ancestors project `proj-a` and
   organization `org-1`.
3. `:1077` calls `MCPServerUse`. The server is private, so it returns one
   request: `mcp:connect` on the target.
4. `:1084` calls `PrepareContext` (`authz/engine.go:112`). It resolves `u-1`
   to `user:u-1`, `user:all` and `role:organization:role-a`, and loads the row
   above.
5. `:1092` calls `Client.Require` (`authz/catalog/client.go:137`), which calls
   `prepare` (`:162`):
   - `mcp:connect` has a definition. ✓
   - The target is one concrete `mcp` resource, not `*`. ✓
   - Its organization `org-1` is the caller's active organization (`:180`). ✓
   - The required ancestors, project and org, are present and in `org-1`
     (`:185`, `requiredAncestor` at `:201`). ✓
   - It builds the check `{scope: mcp:connect, resource_id: srv-a, project_id: proj-a}`
     (`:193`).
6. The backend calls `Engine.Require` (`authz/catalog/backend.go:34`, then
   `authz/engine.go:211`). That reaches `evaluateGrantCheck`
   (`authz/grants.go:402`):
   - `check.expand()` (`authz/access.go:92`) gives the scopes that satisfy
     the check: `root` and `mcp:connect`.
   - `matchingGrant` (`grants.go:437`) compares each row's selector with the
     check (`authz/selector.go:71`): `resource_kind` mcp = mcp, `resource_id`
     `*` matches anything, and `project_id` proj-a = proj-a. **Match.**
   - The exclusion (`authz/checks.go:136`) looks for an `mcp:blocked_connect`
     row matching strictly. There is none. **Allowed.**
7. `decide` (`authz/catalog/client.go:229`) checks where the grant is anchored
   (`anchoredWithin`, `permissions.go:87`). `resource_id` is `*` and
   `project_id` is set, so it is a project-wide grant. `AllowedAnchors` is
   `{mcp}`, so the condition "the grant names one server" is **not met**, and
   the basis is `legacy_broad_grant` (`client.go:244`).
8. `mcp/serveendpoint.go:1100` logs the legacy basis, and the request
   continues to the upstream server.

If project-wide connect should be a normal grant, the change is
`AllowedAnchors: {mcp, project}` (**sketched**). The label then disappears;
nothing else changes.

#### Cases

| Request | Outcome | Why |
| --- | --- | --- |
| connect `srv-a` in `proj-a` | allowed, `legacy_broad_grant` | `*` with `project_id` matches; project anchor not in `AllowedAnchors` |
| connect `srv-c` in `proj-a` | allowed, `legacy_broad_grant` | same row |
| connect `srv-a` in `proj-2` | denied, `no_grant` | `project_id` proj-a ≠ proj-2 at step 6 |

---

### Scenario 2. Role A can manage one server, which also grants view

**Status:** implemented through the catalogue, for the remote server's scope
settings. Other management endpoints in `remotemcp/impl.go` (for example
`:130`, `:173`) still call the engine directly, checking `mcp:write` against
the project rather than the server, so a grant on one server does not reach
them.

#### Grants

| principal_urn | scope | selectors | effect |
| --- | --- | --- | --- |
| `role:organization:role-a` | `mcp:write` | `{"resource_kind": "mcp", "resource_id": "srv-a", "project_id": "proj-a"}` | NULL (allow) |

#### Request

View the server's scope settings (`remotemcp/serverscopes.go:41`, enforced at
`:199`):

```go
s.authorizeServerScopes(ctx, payload.McpServerID, func(target catalog.Target) []catalog.Request {
	return []catalog.Request{catalog.MCPServerRead(target)}
})
// inside: s.authzClient.Require(ctx, requests(serverTarget(authCtx, mcpServerID))...)
```

Manage, which here means changing the scope pin (`remotemcp/serverscopes.go:63`,
and again at `:235` for every server sharing the same upstream URL):

```go
s.authorizeServerScopes(ctx, payload.McpServerID, func(target catalog.Target) []catalog.Request {
	return catalog.MCPScopePin(target, nil) // mcp:write on the server
})
```

#### Rule setup

`authz/catalog/permissions.go:40` and `:49`:

```go
{
	Name:           authz.ScopeMCPRead,
	ImpliedBy:      []authz.Scope{authz.ScopeMCPWrite}, // managing lets you view
	AllowedAnchors: []string{authz.ResourceKindMCP, authz.ResourceKindProject, authz.ResourceKindOrg},
	// ...
},
{
	Name:      authz.ScopeMCPWrite,
	ImpliedBy: nil,
	// ...
},
```

The engine's table (`authz/scopes.go:284`), kept equal by the agreement test:

```go
ScopeMCPRead:    {ScopeMCPWrite},
ScopeMCPConnect: nil, // managing does not let you connect
```

#### Walkthrough

`u-1` views `srv-a`'s scope settings.

1. `GetServerScopes` (`remotemcp/serverscopes.go:40`) calls
   `authorizeServerScopes` (`:189`). That builds the target from the session's
   organization and project (`serverTarget`, `:205`) and asks for
   `MCPServerRead`.
2. `prepare` (`authz/catalog/client.go:162`) passes, as in scenario 1, and
   builds `{scope: mcp:read, resource_id: srv-a, project_id: proj-a}`.
3. `check.expand()` (`authz/access.go:92`) gives `root`, `mcp:read` and
   **`mcp:write`**, because of `authz/scopes.go:284`.
4. `matchingGrant` finds the `mcp:write` row: the scope is in the expanded
   list, `resource_id` srv-a = srv-a, `project_id` proj-a = proj-a.
   **Match.**
5. No `mcp:blocked_read` row. **Allowed.**
6. `decide`: the grant names one server, which is in `AllowedAnchors`, so the
   basis is none.

`u-1` connects to `srv-a`: the steps are the same as scenario 1, but at step
6 `expand()` gives only `root` and `mcp:connect`. The `mcp:write` row's scope
is not in that list, so **no grant matches**, the outcome is `no_grant`, and
`mcp/serveendpoint.go:1098` returns 403 with a request-access link.

#### Cases

| Request | Outcome | Why |
| --- | --- | --- |
| view `srv-a` | allowed | `mcp:read` expands to `mcp:write` |
| manage (pin) `srv-a` | allowed | direct scope match |
| connect `srv-a` | denied, `no_grant` | `mcp:connect` expands to nothing but itself |
| view or manage `srv-c` | denied, `no_grant` | `resource_id` srv-a ≠ srv-c |

---

### Scenario 3. Role B blocks server C; a user in both roles connects to all but C

**Status:** implemented through the catalogue.

#### Grants

`u-1` holds roles A and B, so both rows load:

| principal_urn | scope | selectors | effect |
| --- | --- | --- | --- |
| `role:organization:role-a` | `mcp:connect` | `{"resource_kind": "mcp", "resource_id": "*", "project_id": "proj-a"}` | NULL (allow) |
| `role:organization:role-b` | `mcp:blocked_connect` | `{"resource_kind": "mcp", "resource_id": "srv-c"}` | NULL |

A block is stored as its own scope (`mcp:blocked_connect`), not through the
`effect` column. The engine does not read `effect`: the grant query (`access/queries.sql:13`) does not select it.

#### Request

The same call as scenario 1 (`mcp/serveendpoint.go:1077`, `:1092`).

#### Rule setup

The definition names the block and makes it win (`authz/catalog/permissions.go:58`):

```go
ExcludedBy: authz.ScopeMCPBlockedConnect,
BlockWins:  true,
```

The engine's side, kept equal by the agreement test:

```go
// authz/scopes.go:342: which block withdraws which permission
ScopeMCPConnect: ScopeMCPBlockedConnect,

// authz/precedence.go:51: blocks no allow can outrank
var blockWinsScopes = []Scope{ScopeMCPRead, ScopeMCPWrite, ScopeMCPConnect}
```

A block matches strictly (`authz/checks.go:147`): every key on the block row
must be present in the check and equal.

#### Walkthrough

`u-1` connects to `srv-c`.

1. Steps 1 to 5 of scenario 1. The check is
   `{scope: mcp:connect, resource_id: srv-c, project_id: proj-a}`.
2. `evaluateGrantCheck` (`authz/grants.go:402`): `matchingGrant` finds role
   A's row (`*` in `proj-a`). An allow exists.
3. `expressionForCheck` (`authz/checks.go:136`) builds "allow minus block",
   evaluated in `evaluateGrantCheckDifference` (`authz/expressions.go:278`).
   Role B's row is matched strictly against the check: `resource_kind` mcp =
   mcp and `resource_id` srv-c = srv-c, and the block has no other keys.
   **The block matches.**
4. `directOverride` (`authz/precedence.go:109`) asks whether a direct user
   grant may outrank the block: `ExclusionYieldsToDirectGrants(mcp:connect)`
   (`:93`) is false, because `mcp:connect` is in `blockWinsScopes`. **The block
   stands.**
5. The result is `Blocked`. `decide` returns `ReasonBlocked`, `Require`
   returns 403, and the upstream is never contacted.

For `srv-a`, step 3 compares `resource_id` srv-c with srv-a, the block does
not match, and the request is allowed. Tool calls on `srv-c` are denied the
same way: their check also carries `resource_id: srv-c`, which the block
matches.

#### Cases

| Request | Outcome | Why |
| --- | --- | --- |
| connect `srv-a` | allowed | role A's row; the block names srv-c |
| connect `srv-c` | denied, `blocked` | the block matches; `BlockWins` |
| call a tool on `srv-a` | allowed | |
| call a tool on `srv-c` | denied | the block matches the tool-call check too |
| `u-1` also holds a direct `mcp:connect {resource_id: srv-c}` | still denied | `BlockWins`: no direct-grant override for MCP |

---

### Scenario 4. Role C can connect to every server but only call read-only tools

**Status:** enforced by the engine, outside the catalogue. Has a gap.

#### Grants

| principal_urn | scope | selectors | effect |
| --- | --- | --- | --- |
| `role:organization:role-c` | `mcp:connect` | `{"resource_kind": "mcp", "resource_id": "*", "disposition": "read_only"}` | NULL (allow) |

#### Request

Connecting is the same call as scenario 1. Each tool call is checked by an
interceptor the proxy attaches (`remotemcp/proxymanager.go:366`), which calls
the engine directly, not the catalogue (`remotemcp/tools_call_authz_interceptor.go:90`, `:95`):

```go
dispositions, err := i.resolver.Dispositions(ctx, i.mcpServerID, i.projectID)
err = i.authz.Require(ctx, authz.MCPToolCallCheck(i.mcpServerID, authz.MCPToolCallDimensions{
	Tool:        call.Params.Name,
	Disposition: dispositions[call.Params.Name],
	ProjectID:   i.projectID,
}))
```

`tools/list` uses the same check to hide tools
(`remotemcp/tools_list_mcp_connect_filter_interceptor.go:111`).

#### Rule setup

There is no catalogue entry for tools. The rule lives in the engine:

```go
// authz/selector.go:186: keys an mcp grant may carry
ResourceKindMCP: {SelectorKeyTool: true, SelectorKeyDisposition: true, SelectorKeyProjectID: true},

// authz/checks.go:12: a tool call is an mcp:connect check with extra keys;
// empty values are left out
if dims.Disposition != "" {
	dimensions[SelectorKeyDisposition] = dims.Disposition
}

// conv/tools.go:394: a tool's disposition is its first set hint
case a.ReadOnlyHint != nil && *a.ReadOnlyHint:    return "read_only"
case a.DestructiveHint != nil && *a.DestructiveHint: return "destructive"
// ... idempotent, open_world, otherwise ""
```

The hints come from the server's stored tool annotations, which a project's
tool variations can override (`mcpservers/queries.sql:414`, cached in
`mcpservers/tooldisposition/cache.go:77`).

#### Walkthrough

`u-1` connects to `srv-a`, then calls `delete_page` (annotated destructive),
then `notes` (no annotations).

1. **Connect:** scenario 1's path. The connect check has no `disposition` key.
   In `Selector.matches` (`authz/selector.go:71`), a grant key the check lacks
   is skipped (`:74`), so the row matches. Allowed, labelled
   `legacy_broad_grant` because `resource_id` is `*`.
2. **`delete_page`:** the interceptor (`tools_call_authz_interceptor.go:76`)
   resolves dispositions; `delete_page` is `destructive`. It builds
   `{scope: mcp:connect, resource_id: srv-a, tool: delete_page, disposition: destructive, project_id: proj-a}`
   and calls `Engine.Require` (`authz/engine.go:211`). In `matchingGrant`,
   `disposition` read_only ≠ destructive, so **no match**. The call is refused
   with `ToolPermissionDenied`.
3. **`notes`:** no annotations, so `DispositionFromAnnotations` returns `""`
   and `MCPToolCallCheck` leaves `disposition` out of the check
   (`authz/checks.go:17`). The grant's `disposition` key is skipped
   (`selector.go:74`). **Match: allowed. This is the gap.**

The engine already applies strict matching to tool calls in one place, when a
direct grant tries to outrank a block (`authz/precedence.go:138`), but not to
ordinary allows.

**Sketched (not built):** bring tool calls into the catalogue. Add a
tool-call builder, used by both interceptors in place of
`authz.MCPToolCallCheck`, and let `prepare` refuse a tool with no disposition
as `missing_fact` whenever a disposition is required:

```go
// authz/catalog/mcpserver.go
func MCPToolCall(target Target, tool, disposition string) Request
```

#### Cases

| Request | Outcome | Why |
| --- | --- | --- |
| connect to any server | allowed, `legacy_broad_grant` | the connect check has no disposition, so that key is skipped |
| call a `read_only` tool | allowed | read_only = read_only |
| call a `destructive` or `idempotent` tool | denied | disposition mismatch |
| call a tool with no annotations | **allowed (gap)** | empty disposition is left out of the check |

---

### Scenario 5. Role D can connect to server A and only use tool A

**Status:** enforced by the engine, outside the catalogue.

#### Grants

| principal_urn | scope | selectors | effect |
| --- | --- | --- | --- |
| `role:organization:role-d` | `mcp:connect` | `{"resource_kind": "mcp", "resource_id": "srv-a", "tool": "tool_a"}` | NULL (allow) |

#### Request

Connect: same as scenario 1. Tool calls and `tools/list`: same interceptor
calls as scenario 4.

#### Rule setup

The same engine rules as scenario 4: `tool` is an allowed key on `mcp` grants
(`authz/selector.go:186`), and `MCPToolCallCheck` puts the tool name in the
check (`authz/checks.go:14`). Tool names are compared exactly; there are no
patterns.

#### Walkthrough

`u-1` connects to `srv-a` and calls `tool_b`.

1. **Connect:** the check `{mcp:connect, resource_id: srv-a, project_id: proj-a}`
   has no `tool` key, so the grant's `tool` key is skipped
   (`authz/selector.go:74`) and `resource_id` matches. Allowed. `decide` finds
   a grant that names one server, so the basis is none.
2. **`tools/list`:** the filter interceptor checks each tool and keeps only
   matches (`tools_list_mcp_connect_filter_interceptor.go:125`). `u-1` sees
   only `tool_a`.
3. **`tool_b`:** the check carries `tool: tool_b`. `tool` tool_a ≠ tool_b, so
   no grant matches, and the call is refused.

As in scenario 4, the catalogue does not take part in the tool decision.

#### Cases

| Request | Outcome | Why |
| --- | --- | --- |
| connect `srv-a` | allowed | tool key skipped on a connection check |
| connect `srv-c` | denied | `resource_id` mismatch |
| call `tool_a` on `srv-a` | allowed | |
| call `tool_b` on `srv-a` | denied; hidden from `tools/list` | `tool` mismatch |
| call `tool_a` on `srv-c` | denied | `resource_id` mismatch |
