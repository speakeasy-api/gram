# Option B, step by step

This guide explains option B one step at a time, in plain words, with the code
that implements each step. New steps are added at the end as they are asked
for.

Labels:

- **Implemented**: the code exists on this branch and has tests.
- **Sketched**: proposed code that is not wired in or not tested.

## Steps

1. [The scaffolding: defining checks and injecting the client](#step-1-the-scaffolding)

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
