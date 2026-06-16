# A Guide to RBAC

## Context

Gram's role-based access control (RBAC) system answers one question:

> Is this principal allowed to perform this action on this resource?

Authorization is intentionally explicit. Handlers name the permission they need, the resource they are operating on, and any extra dimensions that narrow the operation. The RBAC engine compares that check against the grants loaded for the current request.

RBAC touches almost every part of the product, so the goal of this guide is to make the model predictable. It should help you choose scopes, understand grants, add enforcement to handlers, and reason about allow and deny cases without needing to rediscover the implementation every time.

The most important design choice is that scopes are defined in code and grants are data. Code defines the vocabulary of things Gram can authorize. The database only records which principals have which scopes, and where those scopes apply.

## Domain Concepts

### Principal

A principal is the subject receiving access. Today, Gram represents principals as URNs in the form:

```text
type:id
```

Examples:

```text
user:user_01abc
role:admin
role:member
role:custom-builder
```

The current RBAC implementation supports `user` and `role` principals, but there is no hard limitation to those two. We can add other principal types as the model grows. For example, we expect to migrate the current API key system into RBAC eventually, which would introduce an `api_key` principal.

A request's effective grants are normally loaded from both:

- the authenticated user principal, such as `user:user_01abc`
- the user's organization role principal, such as `role:admin` or `role:custom-builder`

This lets us give most access through roles while still allowing direct user grants when needed.

### Scope

A scope is a named permission that describes the capability a caller needs. Scopes are the product-facing vocabulary of authorization, so they should be tied to domains that users and admins already understand.

Examples:

```text
org:read
org:admin
project:read
project:write
mcp:read
mcp:write
mcp:connect
```

Think of a scope as:

```text
<domain>:<action>
```

The domain is the kind of thing being protected. The action is what the caller can do to it. For example, `project:read` means the caller can view a project, while `project:write` means the caller can mutate a project or project-owned resource. `mcp:connect` is intentionally separate from `mcp:read` and `mcp:write` because runtime use of an MCP server is a different customer-facing capability from managing or inspecting the server.

Scopes are code, not database rows. The current vocabulary lives in `server/internal/authz/scopes.go`:

```go
const (
	ScopeRoot               Scope = "root"
	ScopeOrgRead            Scope = "org:read"
	ScopeOrgAdmin           Scope = "org:admin"
	ScopeProjectRead        Scope = "project:read"
	ScopeProjectWrite       Scope = "project:write"
	ScopeMCPRead            Scope = "mcp:read"
	ScopeMCPWrite           Scope = "mcp:write"
	ScopeMCPConnect         Scope = "mcp:connect"
	ScopeEnvironmentRead    Scope = "environment:read"
	ScopeEnvironmentWrite   Scope = "environment:write"
	ScopeRiskPolicyEvaluate Scope = "risk_policy:evaluate"
	ScopeRiskPolicyBypass   Scope = "risk_policy:bypass"
)
```

That is deliberate. Adding a scope changes the product contract: it affects role management, the access API, generated SDK types, the dashboard, and tests. Prefer the small action vocabulary we already use: `read` for viewing or listing, `write` for mutation, and `connect` for runtime connection surfaces. New verbs should be rare. If an action needs multiple words, or describes a very specific operation, that is usually a sign that the behavior should be modeled with an existing scope plus selector dimensions instead of a new scope.

For example, prefer `mcp:connect` plus a `tool` selector dimension over a scope like `mcp:call-search-docs`. Prefer `project:write` for project-owned mutations unless customers genuinely need to delegate that mutation separately. In practice, a new scope should exist only when admins need to assign it independently.

Some scopes imply lower-privilege scopes. The expansion rules also live in `server/internal/authz/scopes.go`:

```go
var scopeExpansions = map[Scope][]Scope{
	ScopeRoot:               nil,
	ScopeOrgRead:            {ScopeOrgAdmin},
	ScopeOrgAdmin:           nil,
	ScopeProjectRead:        {ScopeProjectWrite},
	ScopeProjectWrite:       nil,
	ScopeMCPRead:            {ScopeMCPWrite},
	ScopeMCPWrite:           nil,
	ScopeMCPConnect:         {ScopeMCPRead, ScopeMCPWrite},
	ScopeEnvironmentRead:    {ScopeEnvironmentWrite},
	ScopeEnvironmentWrite:   nil,
	ScopeRiskPolicyEvaluate: nil,
	ScopeRiskPolicyBypass:   nil,
}
```

Read this map as "if a handler requires the key scope, any scope in the value list also satisfies the check." A `project:write` grant satisfies a `project:read` check. An `mcp:write` grant satisfies `mcp:read`, and both `mcp:read` and `mcp:write` satisfy `mcp:connect`. `environment:write` satisfies `environment:read`. `org:admin` satisfies `org:read`, and `root` satisfies every check.

Expansion happens when the engine evaluates a check. If code asks for `project:read`, the engine checks for `root`, `project:read`, and `project:write` against the same resource selector. The API also exposes the inverse relationship as `sub_scopes` so role UIs can show what a broader grant implies; those sub-scopes are derived from code and are not additional database rows.

### Grant

A grant gives a principal a scope for some resource selector.

Conceptually:

```text
principal + scope + selector = grant
```

Example:

```text
role:member has project:read on project 018f...
```

In database shape:

```json
{
  "principal_urn": "role:member",
  "scope": "project:read",
  "selectors": {
    "resource_kind": "project",
    "resource_id": "018f..."
  }
}
```

A scope says what operation is allowed. A grant says who receives that scope and where it applies.

### Selector

A selector narrows a grant to the resource it applies to. Every persisted selector must include:

```json
{
  "resource_kind": "project",
  "resource_id": "018f..."
}
```

`resource_kind` is typically derived from the scope family, but there may be some exceptions:

- `org:*` -> `org`
- `project:*` -> `project`
- `mcp:*` -> `mcp`
- `mcp:*` -> `external_mcp` (differentiator between MCPs hosted by us and external ones)
- `root` -> `*`

`resource_id` is the concrete resource ID, or `*` for unrestricted access within the scope.

Examples:

```json
{
  "resource_kind": "project",
  "resource_id": "018f..."
}
```

```json
{
  "resource_kind": "mcp",
  "resource_id": "toolset_123"
}
```

```json
{
  "resource_kind": "project",
  "resource_id": "*"
}
```

Selectors can also include extra dimensions for scope families that allow them. Today, `mcp` selectors can use:

- `tool`
- `disposition`

Example:

```json
{
  "resource_kind": "mcp",
  "resource_id": "toolset_123",
  "tool": "search_docs",
  "disposition": "read_only"
}
```

That grant is narrower than a plain `mcp:connect` grant on the whole toolset.

### Check

A check is what code asks the RBAC engine to enforce. It names the required `Scope`, the `ResourceID` being accessed, and optionally a `ResourceKind` or `Dimensions`. Most checks leave `ResourceKind` empty so it can be derived from the scope, and use dimensions only when the operation needs to be narrower than the resource itself.

Example:

```go
authz.Check{
	Scope:        authz.ScopeProjectWrite,
	ResourceKind: "",
	ResourceID:   authCtx.ProjectID.String(),
	Dimensions:   nil,
}
```

A grant lives in data. A check lives in code. Authorization succeeds when at least one loaded grant satisfies the check.

## Grant Expressions and Set Difference

Some authorization questions are not answered by one grant. They are answered by
starting with a base grant set and subtracting an exclusion grant set:

```text
effective result = base - exclusion
```

This is the same shape as a Zanzibar-style userset difference such as:

```text
viewer = allowed - blocked
```

In Gram, the main example today is risk policy evaluation:

```text
risk_policy_applies =
  risk_policy:evaluate(policy_id)
  - risk_policy:bypass(policy_id, runtime_dimensions)
```

Read that as:

> Apply the policy if the user can evaluate the policy, unless the user also has
> a bypass grant for this exact policy and this exact runtime target.

The most important rule: **exclusion grants do not create access by themselves**.
They only subtract something that the base side already proved.

### Risk Policy Example

Assume the user has these grants:

```text
1. risk_policy:evaluate
   selector: {resource_kind: "risk_policy", resource_id: "policy_123"}

2. risk_policy:bypass
   selector: {
     resource_kind: "risk_policy",
     resource_id: "policy_123",
     server_url: "https://abc.com"
   }
```

There are two related questions, and their answers point in opposite directions:

```text
Question A: Does the bypass grant match this request?
Question B: Does the policy still apply after subtracting bypass?
```

| Request being evaluated                    | Bypass grant matches?                                 | `risk_policy:evaluate - risk_policy:bypass` result                              |
| ------------------------------------------ | ----------------------------------------------------- | ------------------------------------------------------------------------------- |
| `policy_123`, `server_url=https://bcd.com` | No. The bypass is only for `https://abc.com`.         | Policy applies. Base evaluate grant matches, and nothing subtracts it.          |
| `policy_123`, `server_url=https://abc.com` | Yes. Same policy and same server URL.                 | Policy does not apply. The bypass subtracts the exact policy/server instance.   |
| `policy_345`, `server_url=https://abc.com` | No. The bypass is for `policy_123`, not `policy_345`. | Policy does not apply because there is no base evaluate grant for `policy_345`. |

The second row is the core set-difference case:

```text
base set:
  {policy_id: policy_123, server_url: https://abc.com}

exclusion set:
  {policy_id: policy_123, server_url: https://abc.com}

base - exclusion:
  {}
```

The expression result is empty, so the policy is not applied for that request.

The first row keeps the base result:

```text
base set:
  {policy_id: policy_123, server_url: https://bcd.com}

exclusion set:
  {policy_id: policy_123, server_url: https://abc.com}

base - exclusion:
  {policy_id: policy_123, server_url: https://bcd.com}
```

The exclusion is real, but it is for a different concrete permission instance.
It does not subtract the `https://bcd.com` decision.

### MCP Example

The same model can express "allow broad access except a narrow blocked target."
For example:

```text
mcp_tool_call_allowed =
  mcp:connect(toolset_id, tool)
  - mcp:block(toolset_id, tool)
```

Assume the user has:

```text
1. mcp:connect
   selector: {resource_kind: "mcp", resource_id: "toolset_123"}

2. mcp:block
   selector: {
     resource_kind: "mcp",
     resource_id: "toolset_123",
     tool: "delete_database"
   }
```

| Tool call being evaluated             | Block grant matches?                         | `mcp:connect - mcp:block` result                                                       |
| ------------------------------------- | -------------------------------------------- | -------------------------------------------------------------------------------------- |
| `toolset_123`, `tool=search_docs`     | No. The block is only for `delete_database`. | Tool call is allowed.                                                                  |
| `toolset_123`, `tool=delete_database` | Yes. Same toolset and same tool.             | Tool call is not allowed. The block subtracts the exact tool-call instance.            |
| `toolset_456`, `tool=delete_database` | No. The block is for `toolset_123`.          | Tool call is not allowed unless another base `mcp:connect` grant covers `toolset_456`. |

Again, the exclusion side only subtracts. A block/bypass/exclusion grant without
a matching base grant never grants anything.

## Scopes vs Grants

Scopes and grants are easy to confuse, but they answer different questions.

Scopes define the permission vocabulary:

```text
project:write
mcp:connect
org:admin
```

Grants assign that vocabulary to principals:

```text
role:admin has project:write on every project
role:member has mcp:connect on every MCP server
role:custom-support has mcp:connect on toolset_123, tool=search_docs
```

The practical distinction is simple: add a grant when the permission already exists but another principal needs it. Add a scope only when the product needs a new kind of permission that should be independently assignable. Most changes should add or modify grants, not scopes.

## Data Model: Grants

RBAC grants are stored in `principal_grants`.

```sql
CREATE TABLE IF NOT EXISTS principal_grants (
  id uuid NOT NULL DEFAULT generate_uuidv7(),
  organization_id TEXT NOT NULL,
  principal_urn TEXT NOT NULL,
  principal_type TEXT NOT NULL GENERATED ALWAYS AS (split_part(principal_urn, ':', 1)) STORED,
  scope TEXT NOT NULL,
  drop_resource TEXT,
  selectors JSONB NOT NULL,

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),

  CONSTRAINT principal_grants_pkey PRIMARY KEY (id),
  CONSTRAINT principal_grants_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES organization_metadata (id) ON DELETE CASCADE,
  CONSTRAINT principal_grants_selectors_check CHECK (jsonb_typeof(selectors) = 'object' AND selectors != '{}')
);
```

The important columns mirror the authorization model. `organization_id` keeps every grant inside one organization. `principal_urn` identifies the user or role receiving the grant, and `principal_type` is generated from that URN prefix. `scope` stores the permission being granted, while `selectors` stores the JSONB constraints that describe where the grant applies. `drop_resource` is deprecated and scheduled for removal.

There are two important indexes:

```sql
CREATE UNIQUE INDEX IF NOT EXISTS principal_grants_org_principal_scope_selector_key
ON principal_grants (organization_id, principal_urn, scope, selectors);

CREATE INDEX IF NOT EXISTS principal_grants_selectors_idx
ON principal_grants
USING GIN (selectors);
```

The unique index prevents duplicate grants for the same organization, principal, scope, and selector. The GIN index supports selector-oriented query patterns as the model grows.

### Why selectors are JSONB

Selectors need to represent a stable core shape plus optional narrowing fields. Every selector has `resource_kind` and `resource_id`, but some resource types need more detail.

For example, MCP authorization may need to distinguish access to the whole toolset from access to one tool in that toolset, or from access only to tools with a `read_only` disposition. JSONB keeps the table normalized around grants while allowing resource families to introduce narrowly scoped dimensions without adding sparse columns for every possible resource type.

### Wildcards

Wildcard grants use explicit wildcard values. A global wildcard uses:

```json
{
  "resource_kind": "*",
  "resource_id": "*"
}
```

For normal non-root scopes, use a resource-family wildcard instead. Create these through helpers such as `authz.NewSelector(scope, authz.WildcardResource)`, which derives the correct `resource_kind` and sets:

```json
{
  "resource_kind": "project",
  "resource_id": "*"
}
```

That means "all projects" for a `project:*` scope. An `mcp:*` wildcard would use `"resource_kind": "mcp"` and `"resource_id": "*"`.

Do not represent wildcard grants as `{}`. The database rejects empty selector objects, and normal scopes validate that `resource_kind` matches the scope family.

### Query pattern

Grant loading is intentionally simple. For a request, the engine builds the list of principals and loads all matching grants in one query:

```sql
SELECT scope, selectors
FROM principal_grants
WHERE organization_id = @organization_id
  AND principal_urn = ANY(@principal_urns::text[]);
```

The principal list usually contains the user and their role:

```text
user:user_01abc
role:member
```

The engine then evaluates checks in memory against the loaded grants.

## System Roles

Gram ships with two system roles, `admin` and `member`, and their grants are defined in `authz.SystemRoleGrants`.

`admin` receives all standard scopes:

```text
org:admin
org:read
project:read
project:write
mcp:read
mcp:write
mcp:connect
environment:read
environment:write
```

`member` receives read and connect access by default:

```text
org:read
project:read
mcp:read
mcp:connect
environment:read
```

System roles are seeded when RBAC is enabled for an organization. They are not meant to be edited like custom roles. Changing the default grants of a system role is a product behavior change and should be treated carefully, especially for existing organizations.

## Dashboard Grant Reference

Use this table when answering "what grant is required to use this dashboard feature?" It records the dashboard's page and action gates, but server-side checks remain authoritative. When a row lists multiple scopes separated by `OR`, any one of those grants can open the surface. Scope expansion still applies: `org:admin` implies `org:read`, `project:write` implies `project:read`, `mcp:write` implies `mcp:read` and `mcp:connect`, and `environment:write` implies `environment:read`.

Selectors matter. A project-scoped feature needs the grant selector to match the active project. An MCP feature needs the selector to match the target MCP server or toolset. An unrestricted selector for the scope family covers every resource in that family.

| Dashboard feature or question                                                       | Required grant(s)                           | Selector target                                                       | Notes                                                                                                                                                                                                            |
| ----------------------------------------------------------------------------------- | ------------------------------------------- | --------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Open organization home and see project entry points                                 | `org:read` OR `project:read` OR `org:admin` | Org or project selector, depending on grant                           | The page can render for org readers, project readers, or admins. Project visibility still depends on backend filtering and loaded organization data.                                                             |
| Create a project                                                                    | `org:admin`                                 | Organization ID                                                       | The `/rpc/projects.create` handler requires `org:admin`. `project:write` is not enough because the project does not exist yet.                                                                                   |
| Invite members, change member roles, create/update/delete roles                     | `org:admin`                                 | Organization ID                                                       | Access management mutations are organization administration.                                                                                                                                                     |
| Open Access / RBAC page                                                             | `org:read` OR `org:admin`                   | Organization ID                                                       | Viewing access state can be read-only; role, grant, member-role, and challenge-resolution actions are `org:admin`.                                                                                               |
| Open Team page and manage members                                                   | `org:admin`                                 | Organization ID                                                       | The current Team page is page-gated on `org:admin`.                                                                                                                                                              |
| Open Billing                                                                        | `org:read` OR `org:admin`                   | Organization ID                                                       | Billing management sections and portal actions are `org:admin`.                                                                                                                                                  |
| Manage organization API keys or generate agent tokens                               | `org:admin`                                 | Organization ID                                                       | API key listing and creation are admin-only in the dashboard.                                                                                                                                                    |
| View organization domains, org logs, webhooks, identity, audit logs, device agent   | `org:read` OR `org:admin`                   | Organization ID                                                       | Mutating settings on these pages, including domains, forwarding, webhooks, identity providers, and device-agent token generation, requires `org:admin`.                                                          |
| View organization collections                                                       | `org:read` OR `org:admin`                   | Organization ID                                                       | Collection create/update/delete and MCP server attach/detach actions require `org:admin`. Adding collection content to a project can additionally require `project:write` for that project.                      |
| Open project home                                                                   | `project:read`                              | Project ID                                                            | `project:write` also opens it through scope expansion.                                                                                                                                                           |
| Open Sources and source detail pages                                                | `project:read` OR `project:write`           | Project ID                                                            | Creating, importing, reconnecting, deleting, or editing sources requires `project:write`. Creating custom remote MCP servers is gated by `mcp:write`.                                                            |
| Add OpenAPI, add Function, or add from Catalog through Sources                      | `project:write`                             | Project ID                                                            | These routes create project-owned resources.                                                                                                                                                                     |
| Open Catalog                                                                        | `project:read` OR `mcp:write`               | Project ID or MCP selector, depending on entry point                  | Browsing from the main nav can be available to project readers. Adding a catalog server to a project is a write action and is gated at the add flow.                                                             |
| Open Playground                                                                     | `mcp:connect` OR `mcp:read` OR `mcp:write`  | MCP server or toolset ID                                              | Runtime calls should use `mcp:connect`; editing saved playground/server configuration requires `mcp:write`.                                                                                                      |
| Open MCP server/toolset pages                                                       | `mcp:read` OR `mcp:write`                   | MCP server or toolset ID                                              | Creating, editing, deleting, publishing, authentication, team-access, prompt/resource, OAuth, tool filtering, and settings actions generally require `mcp:write`; collection publishing can require `org:admin`. |
| Create a custom remote MCP server                                                   | `mcp:write`                                 | MCP selector, usually unrestricted or target server ID after creation | The create route is page-gated on `mcp:write`.                                                                                                                                                                   |
| Open Deployments                                                                    | `project:read` OR `project:write`           | Project ID                                                            | Deployment-triggering and failed-source retry actions require `project:write`.                                                                                                                                   |
| Open Assistants                                                                     | `project:read`                              | Project ID                                                            | Creating or editing assistants is gated by `project:write` OR `mcp:write`; admin-only assistant management sections are `org:admin`.                                                                             |
| Open Skills / CLIs                                                                  | `project:read`                              | Project ID                                                            | This is a read-only project surface.                                                                                                                                                                             |
| Open Plugins                                                                        | `project:read` OR `project:write`           | Project ID                                                            | Plugin install, update, delete, or configuration actions require `project:write`.                                                                                                                                |
| Open Environments list                                                              | `project:read` OR `project:write`           | Project ID                                                            | Creating a new environment is currently `project:write`; environment card clone actions are `environment:write`.                                                                                                 |
| Open an Environment detail page                                                     | `project:read`                              | Project ID                                                            | Adding, editing, deleting, or filling variables is currently `project:write`. Environment-specific clone checks use `environment:read` for the source and `environment:write` for the destination.               |
| Open Insights                                                                       | `project:read`                              | Project ID                                                            | Some nested insights routes accept `project:write` too, but `project:read` is the intended read grant.                                                                                                           |
| Open Logs / Agent Sessions                                                          | `project:read`                              | Project ID                                                            | Project log tabs are read surfaces. Risk-event logs are `org:admin`.                                                                                                                                             |
| Open Project Settings                                                               | `project:write`                             | Project ID                                                            | Settings are treated as project mutation/admin surface.                                                                                                                                                          |
| Open Secure pages: Risk Overview, Risk Policies, Approval Requests, Detection Rules | `org:admin`                                 | Organization ID                                                       | The dashboard pages are admin-only today. `risk_policy:evaluate` and `risk_policy:bypass` are policy runtime/request-flow scopes, not general dashboard page grants.                                             |

## Enforcing RBAC in Code

### Request preparation

Before handlers run, the auth middleware prepares an auth context. RBAC grant loading is handled by `authz.Engine.PrepareContext`. For an eligible request, the engine reads the auth context, builds principal URNs for the user and their organization role, loads the matching rows from `principal_grants`, and stores those grants back on the request context.

Handlers do not query grants directly. Their job is to describe the access they need and ask the engine to enforce it.

### When RBAC is enforced

`authz.Engine.ShouldEnforce` decides whether checks should actually be applied. Today, RBAC is enforced for authenticated enterprise requests when the RBAC feature flag is enabled for the active organization, as long as the request is not using an API key. The request also needs a session, except for assistant-token requests, which are allowed through this path.

Scope overrides are a special case. In local development, authenticated users can use override headers. In production, only superadmins can. When valid overrides are present, RBAC is enforced so the overridden grant set is what the request experiences.

That means RBAC is not currently enforced for API key requests, non-enterprise accounts, or organizations where the RBAC feature flag is disabled. Unauthenticated contexts are handled as authorization errors by the normal auth path. This may change as the RBAC model expands, especially when API keys move into RBAC.

### Add checks at the handler boundary

The normal pattern is to check authorization near the top of a handler, before doing work with side effects.

Project write example:

```go
if err := s.authz.Require(ctx, authz.Check{
	Scope:        authz.ScopeProjectWrite,
	ResourceKind: "",
	ResourceID:   authCtx.ProjectID.String(),
	Dimensions:   nil,
}); err != nil {
	return nil, err
}
```

For most checks, leave `ResourceKind` empty so it is derived from the scope, set `ResourceID` to the concrete resource being protected, and leave `Dimensions` nil. The exception is when the handler intentionally needs a more specific resource kind, such as external MCP, or needs to narrow the check with dimensions.

The engine rejects empty resource IDs and wildcard resource IDs in checks. Wildcards are for grants, not for runtime checks.

### Dimensions

Dimensions are extra selector fields that narrow a check beyond resource kind and resource ID.

Today, dimensions are used for MCP tool calls:

```go
if err := authzEngine.Require(ctx, authz.MCPToolCallCheck(toolset.ID, authz.MCPToolCallDimensions{
	Tool:        params.Name,
	Disposition: disposition,
})); err != nil {
	return nil, err
}
```

That check becomes a selector like:

```json
{
  "resource_kind": "mcp",
  "resource_id": "toolset_123",
  "tool": "search_docs",
  "disposition": "read_only"
}
```

MCP disposition values come from tool annotations and are normalized to:

```text
read_only
destructive
idempotent
open_world
```

Only add a dimension when the resource family needs finer-grained grants without creating a new scope for every variation. New dimensions must be explicitly allowed in `authz.ValidateSelector`; otherwise role grants using them will be rejected.

### Selector matching

A grant selector satisfies a check selector when every key constrained by the grant either equals the check value or is the wildcard `*`.

Examples:

Grant:

```json
{
  "resource_kind": "project",
  "resource_id": "*"
}
```

Check:

```json
{
  "resource_kind": "project",
  "resource_id": "018f..."
}
```

Result: allowed.

Grant:

```json
{
  "resource_kind": "mcp",
  "resource_id": "toolset_123",
  "tool": "search_docs"
}
```

Check:

```json
{
  "resource_kind": "mcp",
  "resource_id": "toolset_123",
  "tool": "delete_customer"
}
```

Result: denied.

One nuance: if a grant has a key that the check does not include, matching skips that key. This allows a more specific grant to satisfy a broader connection-level check that does not yet constrain that dimension. Be aware of this when deciding whether a handler should use a plain resource check or a dimensional check.

### `Require`

Use `Require` when the caller must satisfy every check passed to it.

Example from toolset cloning:

```go
if err := s.authz.Require(
	ctx,
	authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil},
	authz.Check{Scope: authz.ScopeMCPRead, ResourceKind: "", ResourceID: originalToolset.ID.String(), Dimensions: nil},
); err != nil {
	return nil, err
}
```

This requires both write access where the clone will be created and read access to the source toolset. That is the right shape for operations that combine multiple resources or multiple permissions. Cloning is not just "read source" and not just "write destination"; it needs both.

### `RequireAny`

Use `RequireAny` when several scopes are legitimate alternatives and any one of them should authorize the operation.

For example, if a future handler could be reached by either an organization admin or a project writer, it could express that as two checks and require any one of them:

```go
if err := s.authz.RequireAny(
	ctx,
	authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil},
	authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil},
); err != nil {
	return nil, err
}
```

Do not use `RequireAny` to avoid deciding what permission an operation needs. Use it only when the alternatives are intentionally equivalent authorization paths.

### `Filter`

Use `Filter` for list endpoints where the caller may have access to some resources but not all of them.

Example shape:

```go
checks := make([]authz.Check, len(projectIDs))
for i, id := range projectIDs {
	checks[i] = authz.Check{
		Scope:        authz.ScopeProjectRead,
		ResourceID:   id,
		ResourceKind: "",
		Dimensions:   nil,
	}
}

allowedProjectIDs, err := s.authz.Filter(ctx, checks)
if err != nil {
	return nil, err
}
```

Use `Filter` when the correct result is "return the subset this caller can see." Use `Require` when the correct result is "deny the whole operation unless the caller can access this resource."

## Choosing the Right Scope in Handlers

Use the narrowest scope that describes the operation. GET and list handlers usually need a `*:read` scope. Mutations usually need `*:write`. Runtime MCP usage needs `mcp:connect`, while role and RBAC management usually need `org:admin`.

A few operations deserve extra thought. Creating a project-owned child resource usually requires `project:write`, even if the child resource has its own read scope. Cloning a toolset needs destination write access and source read access. Calling a private MCP tool should use `mcp:connect`, narrowed by tool and disposition when available. Listing resources should usually use `Filter`, because the correct behavior is often to return the visible subset instead of failing the whole list because one resource is inaccessible.

## Examples

### Example 1: Project reader can view one project

Grant row:

```json
{
  "organization_id": "org_123",
  "principal_urn": "role:analyst",
  "scope": "project:read",
  "selectors": {
    "resource_kind": "project",
    "resource_id": "project_a"
  }
}
```

Handler check:

```go
authz.Check{
	Scope:        authz.ScopeProjectRead,
	ResourceKind: "",
	ResourceID:   "project_a",
	Dimensions:   nil,
}
```

Result: allowed.

The scope matches, and the selector matches `project_a`.

### Example 2: Same reader cannot view another project

Grant row:

```json
{
  "principal_urn": "role:analyst",
  "scope": "project:read",
  "selectors": {
    "resource_kind": "project",
    "resource_id": "project_a"
  }
}
```

Handler check:

```go
authz.Check{
	Scope:        authz.ScopeProjectRead,
	ResourceKind: "",
	ResourceID:   "project_b",
	Dimensions:   nil,
}
```

Result: denied.

The scope matches, but the selector does not.

### Example 3: Project writer can read the project

Grant row:

```json
{
  "principal_urn": "role:builder",
  "scope": "project:write",
  "selectors": {
    "resource_kind": "project",
    "resource_id": "project_a"
  }
}
```

Handler check:

```go
authz.Check{
	Scope:        authz.ScopeProjectRead,
	ResourceKind: "",
	ResourceID:   "project_a",
	Dimensions:   nil,
}
```

Result: allowed.

`project:write` satisfies `project:read` through scope expansion.

### Example 4: MCP connect grant for one toolset

Grant row:

```json
{
  "principal_urn": "role:agent-user",
  "scope": "mcp:connect",
  "selectors": {
    "resource_kind": "mcp",
    "resource_id": "toolset_a"
  }
}
```

Handler check:

```go
authz.MCPToolCallCheck("toolset_a", authz.MCPToolCallDimensions{
	Tool:        "search_docs",
	Disposition: "read_only",
})
```

Result: allowed.

The grant does not constrain `tool` or `disposition`, so it covers any tool call inside `toolset_a`.

### Example 5: MCP grant for one tool only

Grant row:

```json
{
  "principal_urn": "role:agent-user",
  "scope": "mcp:connect",
  "selectors": {
    "resource_kind": "mcp",
    "resource_id": "toolset_a",
    "tool": "search_docs"
  }
}
```

Allowed check:

```go
authz.MCPToolCallCheck("toolset_a", authz.MCPToolCallDimensions{
	Tool:        "search_docs",
	Disposition: "read_only",
})
```

Denied check:

```go
authz.MCPToolCallCheck("toolset_a", authz.MCPToolCallDimensions{
	Tool:        "delete_customer",
	Disposition: "destructive",
})
```

The first check matches the constrained tool. The second does not.

### Example 6: Filtering a project list

Grant rows:

```json
[
  {
    "principal_urn": "role:analyst",
    "scope": "project:read",
    "selectors": {
      "resource_kind": "project",
      "resource_id": "project_a"
    }
  },
  {
    "principal_urn": "role:analyst",
    "scope": "project:read",
    "selectors": {
      "resource_kind": "project",
      "resource_id": "project_c"
    }
  }
]
```

Candidate projects:

```text
project_a
project_b
project_c
```

Filter checks:

```go
[]authz.Check{
	{Scope: authz.ScopeProjectRead, ResourceID: "project_a", ResourceKind: "", Dimensions: nil},
	{Scope: authz.ScopeProjectRead, ResourceID: "project_b", ResourceKind: "", Dimensions: nil},
	{Scope: authz.ScopeProjectRead, ResourceID: "project_c", ResourceKind: "", Dimensions: nil},
}
```

Result:

```text
project_a
project_c
```

`Filter` returns the IDs the caller can access. The handler then rebuilds the response using only those IDs.

## Practical Rules

When adding RBAC to a handler:

1. Decide whether the handler should be all-or-nothing (`Require`) or subset-returning (`Filter`).
2. Choose the narrowest existing scope.
3. Use the concrete resource ID being protected.
4. Add dimensions only when the grant model already supports them or you are deliberately extending it.
5. Add tests for both allowed and denied cases.

When adding a new scope:

1. Confirm a customer needs to assign it independently.
2. Add the scope constant in `authz/scopes.go`.
3. Add scope expansion rules.
4. Update system role defaults if appropriate.
5. Update the access API scope metadata and Goa enums.
6. Regenerate server and SDK code.
7. Update the dashboard scope type mirror.
8. Update tests that assert the full scope list.

When adding a new selector dimension:

1. Prefer a typed helper like `MCPToolCallCheck` instead of raw maps at call sites.
2. Add the dimension to `allowedSelectorKeys`.
3. Validate allowed values if the dimension has an enum.
4. Update API and dashboard types if the dimension is user-facing.
5. Add allow and deny tests showing how selector matching should behave.

## Final Mental Model

RBAC in Gram is a small set of moving pieces. Scopes define what can be done. Selectors define where a grant applies. Principals receive grants. Handlers create checks. The engine compares the request's grants to the handler's checks.

Keep scopes coarse and customer-meaningful. Use selectors and dimensions for resource-specific narrowing. Put authorization checks close to the handler boundary. Use `Filter` for partial visibility. Treat every new scope as a product contract, not just a code constant.

## Resources

Please refer to the original [RFC](https://www.notion.so/speakeasyapi/RFC-Gram-RBAC-Scope-Permission-Design-319726c497cc8177b7e9dea65a91ff10) which contains the original decision making process during RBAC
planning. Please note that this might be slightly outdated, and this guide should be the actual source of truth.
