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

Every outcome below is asserted by a test in `scenarios_test.go`. Grants are
written as the selector stored in `principal_grants`; `S*` means "any server".

| # | Scenario | Can option B express it? |
| --- | --- | --- |
| 1 | Role A can connect to every server in project A | **Yes**, and it works. B flags it as a legacy broad grant, because slice 1 defined `mcp:connect` per server. |
| 2 | Role A can manage one server in project A, which also lets it view it | **Yes**, implemented. |
| 3 | Role B blocks server C; a user in roles A and B connects to everything except C | **Yes**, implemented. |
| 4 | Role C can connect to every server but only call read-only tools | **Partly.** The engine enforces it today, but the catalogue does not model tools, and a tool with no annotations gets through. |
| 5 | Role D can connect to server A and call only tool A | **Yes, in the engine.** The catalogue does not model tools. |

Scenarios 1 to 3 are about the connection, which goes through the catalogue.
Scenarios 4 and 5 restrict individual tool calls. Those checks happen in the
tool interceptors (`remotemcp/tools_call_authz_interceptor.go:95`, plus the
tools/list filter), which call the engine directly with the tool name and its
disposition. The catalogue is not involved in them yet.

### Scenario 1. Connect to every server in project A

**Implemented, with a label.**

Stored grant:

```
role A   mcp:connect   {resource_id: "*", project_id: "A"}
```

Definition used: `mcp:connect` (`permissions.go`): `AllowedAnchors: {mcp}`,
`RequiredAncestors: {project, org}`.

| Request | Answer |
| --- | --- |
| connect to server A in project A | allowed, basis `legacy_broad_grant` |
| connect to server C in project A | allowed, basis `legacy_broad_grant` |
| connect to server A in project 2 | denied: the grant's `project_id` does not match the server's project |

How it is decided: the connect check carries the server's verified project
(from its row), so the `project_id` on the grant must match. The wildcard
`resource_id` covers every server in that project.

Why the label: in slice 1 the `mcp:connect` definition allows a grant to name
one server only (`AllowedAnchors: {mcp}`), on the reading that "use" should be
granted per server. A project-wide connect grant still allows, but every allow
is marked `legacy_broad_grant` and logged, so it can be found and migrated. If
"every server in a project" should be a normal grant, the change is one line,
and the label disappears (**sketched**):

```go
AllowedAnchors: []string{authz.ResourceKindMCP, authz.ResourceKindProject},
```

### Scenario 2. Manage one server, which also grants view

**Implemented.**

Stored grant:

```
role A   mcp:write   {resource_id: "S1", project_id: "A"}
```

Definitions used: `mcp:write`, and `mcp:read` with `ImpliedBy: {mcp:write}`.

| Request | Answer |
| --- | --- |
| manage (write) S1 | allowed |
| view (read) S1 | allowed: `mcp:read` lists `mcp:write` in `ImpliedBy` |
| connect to S1 | **denied**: `mcp:connect` has an empty `ImpliedBy`, so managing never grants use |
| view or manage S2 | denied: the grant names S1 only |

The "this also grants view" rule is the one line `ImpliedBy:
[]authz.Scope{authz.ScopeMCPWrite}` on the `mcp:read` definition. If managing
should also grant use, `mcp:connect` would list `mcp:write` there. Slice 1
removed that on purpose.

### Scenario 3. Block one server for a user in two roles

**Implemented.**

Stored grants:

```
role A   mcp:connect           {resource_id: "*", project_id: "A"}
role B   mcp:blocked_connect   {resource_id: "C"}
```

A user who holds both roles gets the union of their grants.

| Request | Answer |
| --- | --- |
| connect to server A | allowed (role A) |
| connect to server C | **denied, reason `blocked`** |
| call any tool on server A | allowed |
| call any tool on server C | denied: the block also matches tool-call checks on C |
| same user also holds a direct `mcp:connect {resource_id: "C"}` | still denied |

How it is decided: `mcp:connect` has `ExcludedBy: mcp:blocked_connect` and
`BlockWins: true`. The engine subtracts any matching block from the allows,
and with `BlockWins` no allow outranks it, not even a grant made directly to
the user. Blocks match strictly: every key on the block must be present on the
check, so a block on C cannot leak onto other servers.

### Scenario 4. Connect to every server but only call read-only tools

**Partly supported. Enforced by the engine today, not modelled by the catalogue.**

Stored grant:

```
role C   mcp:connect   {resource_id: "*", disposition: "read_only"}
```

| Request | Answer |
| --- | --- |
| connect to any server | allowed, basis `legacy_broad_grant` (wildcard connect, see scenario 1) |
| call a tool annotated read-only | allowed |
| call a tool annotated destructive or idempotent | denied |
| **call a tool with no annotations** | **allowed: a gap** |

How it is decided: for each tool call, the interceptor works out the tool's
**disposition** from the annotations the MCP server declares
(`dispositionFromAnnotations`, `mcp/rpc_tools_list.go:328`): `read_only`,
`destructive`, `idempotent` or `open_world`, taking the first hint that is set.
It sends that in the check, and the grant's `disposition` must match.

The gaps:

- **Unannotated tools pass.** A tool with no annotations has no disposition,
  so the check leaves that key out. Allow grants match loosely: a key on the
  grant that the check leaves out is skipped (`Selector.Matches`,
  `authz/selector.go:67`). So the read-only grant also allows the unannotated
  tool. The engine already closes this hole for one case, a direct grant
  outranking a block (`authz/precedence.go:138`), but not for plain allows.
  The test asserts the current behaviour so the gap is visible.
- **The server declares its own annotations.** "Read-only" means what the
  upstream server says about its tool. Speakeasy cannot verify it.
- **The connection itself is not restricted**, which is intended: the user
  can connect and list tools, and only the calls are filtered.

How B could model it (**sketched**, not built): give `Definition` a list of
the tool-call keys it understands, and add a tool-call builder that the
interceptors would use instead of `authz.MCPToolCallCheck`.

```go
// permissions.go
ToolDimensions: []string{authz.SelectorKeyTool, authz.SelectorKeyDisposition},

// mcpserver.go
func MCPToolCall(target Target, tool, disposition string) Request
```

With that in place, `prepare` could treat an unknown disposition as
`missing_fact` (unresolved, never allows) whenever a grant constrains
disposition. That would close the gap in one place instead of in every
interceptor.

### Scenario 5. Connect to server A and call only tool A

**Supported by the engine today, not modelled by the catalogue.**

Stored grant:

```
role D   mcp:connect   {resource_id: "A", tool: "tool_a"}
```

| Request | Answer |
| --- | --- |
| connect to server A | allowed (basis none: it names one server) |
| connect to server C | denied |
| call `tool_a` on server A | allowed |
| call `tool_b` on server A | denied; tools/list also hides it |
| call `tool_a` on server C | denied |

How it is decided: the connection check has no tool, so the grant's `tool`
key is skipped and the connection is allowed. Each tool call carries the tool
name, which must equal `tool_a`. Tool names are matched exactly; there are no
patterns.

Same caveat as scenario 4: the catalogue does not know about tools, so the
tool restriction relies on the interceptors building the right engine check.
The sketch under scenario 4 would bring it into the catalogue.
