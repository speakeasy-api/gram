# A Guide to RBAC

## Context

Speakeasy's role-based access control (RBAC) system answers one question:

> Is this principal allowed to perform this action on this resource?

Authorization is intentionally explicit. Handlers name the permission they need, the resource they are operating on, and any extra dimensions that narrow the operation. The RBAC engine compares that check against the grants loaded for the current request.

RBAC touches almost every part of the product, so the goal of this guide is to make the model predictable. It should help you choose scopes, understand grants, add enforcement to handlers, and reason about allow and deny cases without needing to rediscover the implementation every time.

The most important design choice is that scopes are defined in code and grants are data. Code defines the vocabulary of things Speakeasy can authorize. The database only records which principals have which scopes, and where those scopes apply.

## Domain Concepts

### Principal

A principal is the subject receiving access. Today, Speakeasy represents principals as URNs in the form:

```text
type:id
```

Examples:

```text
user:user_01abc
role:global:00000000-0000-0000-0000-000000000001
role:global:00000000-0000-0000-0000-000000000002
role:organization:00000000-0000-0000-0000-000000000003
agent:00000000-0000-0000-0000-000000000004
```

Role principals use `role:<kind>:<role-uuid>`, where `kind` is `global` or `organization`. Role slugs such as `admin` remain display and lookup metadata; they are not principal identifiers.

The current RBAC implementation supports `user`, `role`, `agent`, and `api_key` principals. An `api_key:<api_keys.id>` principal is a legacy API key holding its own server and auth client policy; it is written only through `keys.create` and is not grantable anywhere else (see "API key policies and the mandatory check").

Agent principals are grantable in two places in the RBAC administration surfaces: on a role, through the role editor, and on a single MCP server, through that server's access list. An agent's own direct grants are managed separately, through the agent policy API (`agents.createPolicyGrant` and friends), which is also what agent key issuance delegates from. Role membership for agents lives in `agent_role_assignments` rather than `organization_role_assignments`, because agents have no WorkOS identity and their membership is never reconciled outward.

Initial direct policy grants in `agents.create`, and later writes through `agents.createPolicyGrant` and `agents.updatePolicyGrant`, must fit within both the configuring person's and current owner's effective permissions. Ownership or `agent:write` permits configuration, not delegation of resource access the caller or owner lacks. Each write checks live tenant-bound grants, including implied scopes, selector dimensions, wildcards, exclusions, and direct-grant precedence. A broad allow is rejected when it overlaps a resource or tool block that remains effective after applying direct-grant precedence; choose a narrower allow instead. A direct concrete grant can override an inherited block, but not the user's own block. Existing policies are not rewritten, and credential issuance retains its independent containment checks.

Policy-write denials use the existing forbidden error envelope. The message distinguishes `your permissions` from `the owner's permissions` only after authorizing management of the selected agent. It suggests narrowing the requested scope or asking an administrator to review access, without returning resource names, IDs, grants, or hidden settings. Clients should display the message rather than treat its wording as a machine-readable code. Inactive owners and unauthorized or cross-tenant targets receive the ordinary forbidden response. Rejected writes roll back policy, agent-creation, audit, and webhook state together. Policy deletion remains available to authorized managers even when the policy no longer fits the human permission boundaries.

Direct policy management remains a human-session-only workflow. Platform MCP intentionally does not expose these policy mutations or credential issuance; this containment change does not broaden its audiences or the separate Assistant identity-upgrade workflow.

An agent's grants — its own and the ones it inherits from its roles — are filtered against the agent runtime scope registry (`server/internal/agents/runtimepolicy/scopes.go`) every time they are loaded. Scopes outside that allowlist are dropped rather than honoured, so a role cannot hand an agent a scope it could not have been granted directly. `access.listScopes` reports this per scope as `agent_eligible` so the role editor can mark the difference instead of leaving it invisible. The same rule is why the `*:blocked_*` scopes cannot be written against an agent: nothing would enforce them.

Widening an agent through a role does not raise its ceiling. A request authenticated as an agent still evaluates the intersection of its credential's delegated policy, its agent policy (now including role grants), and its owner's grants.

Workload sessions use the same three independent boundaries: the session's immutable approved policy, the assigned agent's current policy, and the current owner's effective permissions. Admission reads the assignment, agent, owner membership, and policies in one database snapshot on every request. It does not cache owner permissions between requests. Owner permissions include role and `user:all` grants, exclusions, and direct-grant precedence; they do not become grants held by the workload itself.

This restricts existing workload sessions without changing workload authentication. Removing an owner's permission prevents the next operation from using it. Restoring permission can restore access only within the original session ceiling and the current agent policy. Transfers immediately use the new owner's permissions, not the former owner's. A missing or inactive owner, an unresolved assignment, or an unusable agent fails admission with the ordinary unauthorized response. An eligible owner who lacks a required permission produces a forbidden operation instead.

Configured Assistant triggers that use workload identity follow this boundary too, including autonomous turns without a human authorizer. Assistants without configured agent identity retain their existing owner-scoped authentication. No credential format, issuer setup, or stored policy migration is required. Administrators should review the current owner's access if an existing workload can authenticate but can no longer reach a resource.

MCP session withdrawal rules still apply. A session limited to one MCP server is withdrawn when it loses access to that server; restoring permission then requires a new session. An issuer-wide session can remain usable at other servers its three policies still permit.

Platform MCP needs no new tool for this enforcement change: the target is a runtime operation, and the actor is the authenticated workload or agent. The existing Assistant identity-upgrade tool still reports configuration state, not execution permission; its `ACTIVE` qualification remains accurate. Runtime authorization and configured-trigger regression tests verify the boundary. No new dashboard data requires demo seed changes.

A request's effective grants are normally loaded from both:

- the authenticated user principal, such as `user:user_01abc`
- the user's canonical role principal, such as `role:global:<role-uuid>` or `role:organization:<role-uuid>`

This lets us give most access through roles while still allowing direct user grants when needed. A direct grant naming a concrete resource also outranks a block the user inherits from a role or `user:all`; see [Principal Precedence](#principal-precedence).

An agent-authenticated request loads the same way but from the agent's own set: the agent principal, such as `agent:<agent-uuid>`, plus the role principals recorded for it in `agent_role_assignments`. It never loads `user:all`, its owner's user principal, or its owner's roles into that set — those are evaluated as a separate policy the request is also bounded by.

### Scope

A scope is a named permission that describes the capability a caller needs. Scopes are the product-facing vocabulary of authorization, so they should be tied to domains that users and admins already understand.

Examples:

```text
org:read
org:admin
project:read
project:write
assistant:read
assistant:write
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
	ScopeAssistantRead      Scope = "assistant:read"
	ScopeAssistantWrite     Scope = "assistant:write"
	ScopeMCPRead            Scope = "mcp:read"
	ScopeMCPWrite           Scope = "mcp:write"
	ScopeMCPConnect         Scope = "mcp:connect"
	ScopeEnvironmentRead    Scope = "environment:read"
	ScopeEnvironmentWrite   Scope = "environment:write"
	ScopeSkillRead          Scope = "skill:read"
	ScopeSkillWrite         Scope = "skill:write"
	ScopePluginWrite        Scope = "plugin:write"
	ScopeRiskPolicyEvaluate Scope = "risk_policy:evaluate"
	ScopeRiskPolicyBypass   Scope = "risk_policy:bypass"
	ScopeRiskPolicyBlock    Scope = "risk_policy:block"
	ScopeChatRead           Scope = "chat:read"
	ScopeChatWrite          Scope = "chat:write"
	ScopeLogRead            Scope = "log:read"
)
```

Most Observe dashboard pages (Costs, MCP & Tools Insights, and Agent Sessions) are gated on **`org:admin`** at the page level (no dedicated scope). Tool Logs is gated on **`log:read`** for the active project instead (see below). The Identities roster that replaces Employee Enrollment requires **`project:read`**; identity details separately require **`org:read`** because identity resolution includes organization directory data. The per-employee Shadow AI request requires **`project:read`** on the active project and one employee email, so lower-privilege callers cannot broaden it into an organization-wide inventory. The Observe nav group stays visible to project members; opening an admin-only page as a non-admin renders the page-level "Access restricted" notice.

`log:read` protects a project's individual tool calls and raw log records: `telemetry.searchLogs`, `telemetry.searchToolCalls`, `telemetry.listToolUsageTraces`, and `telemetry.listHooksTraces`, which back the Tool Logs page, the Playground logs panel, and a session's Tool calls view. Those rows carry every member's tool arguments, results (when the organization records tool input and output), prompt text, and hook payloads, so `log:read` is not a default for any system role, not even `admin`, and neither `project:*` nor `org:admin` implies it. Grant it per project through a custom role. Aggregate telemetry (Insights, project overview, MCP server activity) stays on `project:read`. Legacy API keys skip RBAC and keep their existing access; only organization admins can create them.

Agents and assistants follow the same rule. `log:read` is not agent-runtime-safe, so an agent's runtime policy can never carry it, and an agent-backed assistant turn never holds it. The assistant tools that return individual calls (`platform_search_logs`, `platform_search_tool_calls`, and the Platform MCP `search_tool_calls`, `list_recent_tool_calls`, and `query_mcp_traces`) are therefore hidden from any assistant turn without `log:read` on its project, and a direct call returns not found. Where a log tool is still reachable without the grant it returns a structured `log_read_required` response telling the model how the user can get access. The project guide detects its first governed call and blocked secret from `project:read` activity counts and risk findings, so it works without `log:read`.

`chat:read` protects reading _other members'_ agent session transcripts (`chat.load`, `chat.summarize`) and pinning them (`chat.setPinned`). `chat:write` protects destructive mutations — `chat.delete`, `chat.submitFeedback`, and the rename path of `chat.generateTitle`. Neither is a default for any system role — not even `admin` — because session transcripts are sensitive and deleting one is destructive; both must be granted explicitly via a custom role grant.

The two are separate so a session reviewer can be given `chat:read` alone: they can open every transcript in the project and pin one as a shared bookmark, but cannot delete or otherwise alter one. `chat:write` satisfies `chat:read` through the normal expansion rules, so a role that needs both only has to carry `chat:write`.

Everyone (members and admins alike) can always read _and_ mutate sessions they own: the chat handlers bypass the scope check when the caller owns the session (`chat.user_id == caller`), and the session list filters to the caller's own sessions when they lack `chat:read`. Reaching another user's session — or an ownerless/external session — requires the corresponding unrestricted grant.

Onboarding makes that gap explicit rather than leaving an admin waiting on it. Sessions delivered by an Anthropic inference hook are attributed to the email the Claude account reports, which need not be the one the admin signed in to Speakeasy with — so the Confirm traffic step on the Anthropic observability card can sit on Waiting forever for a conversation the admin can't see. The Enable logging step therefore says that an admin sees only their own chats by default, and offers to create a **Session Auditor** custom role carrying unrestricted `chat:read` and add the caller to it under their own `org:admin` session. Both writes go through the ordinary role endpoints (`access.createRole`, `access.updateMemberRoles`) and are audited: creating the role with the caller on it emits `access_role:create`, while assigning an existing role and removing the membership each emit `access_member:update_role`; the Confirm traffic step offers to take the role back off the caller once traffic has arrived. The role itself is never deleted — it stays an ordinary custom role, reused by the next admin who needs it. Under directory sync the role is still created, because the identity provider needs something to map a group to, but the membership is left to the identity provider: a self-assignment would be replaced on the next `organization_membership.*` reconciliation.

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
	ScopeAssistantRead:      {ScopeAssistantWrite},
	ScopeAssistantWrite:     nil,
	ScopeMCPRead:            {ScopeMCPWrite},
	ScopeMCPWrite:           nil,
	ScopeMCPConnect:         {ScopeMCPRead, ScopeMCPWrite},
	ScopeEnvironmentRead:    {ScopeEnvironmentWrite},
	ScopeEnvironmentWrite:   nil,
	ScopeSkillRead:          {ScopeSkillWrite},
	ScopeSkillWrite:         nil,
	ScopeRiskPolicyEvaluate: nil,
	ScopeRiskPolicyBypass:   nil,
	ScopeChatRead:           {ScopeChatWrite},
	ScopeChatWrite:          nil,
	ScopeLogRead:            nil,
}
```

Read this map as "if a handler requires the key scope, any scope in the value list also satisfies the check." A `project:write` grant satisfies a `project:read` check. An `assistant:write` grant satisfies `assistant:read`. An `mcp:write` grant satisfies `mcp:read`, and both `mcp:read` and `mcp:write` satisfy `mcp:connect`. `environment:write` satisfies `environment:read`, `skill:write` satisfies `skill:read`, and `chat:write` satisfies `chat:read`. `org:admin` satisfies `org:read`, and `root` satisfies every check.

Expansion happens when the engine evaluates a check. If code asks for `project:read`, the engine checks for `root`, `project:read`, and `project:write` against the same resource selector. The API also exposes the inverse relationship as `sub_scopes` so role UIs can show what a broader grant implies; those sub-scopes are derived from code and are not additional database rows.

### Grant

A grant gives a principal a scope for some resource selector.

Conceptually:

```text
principal + scope + selector = grant
```

Example:

```text
role:global:00000000-0000-0000-0000-000000000002 has project:read on project 018f...
```

In database shape:

```json
{
  "principal_urn": "role:global:00000000-0000-0000-0000-000000000002",
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
- `assistant:*` -> `assistant` (the `resource_id` is the assistant UUID, or `*` with a `project_id` dimension for every assistant in a project; see [Assistant scopes](#assistant-scopes))
- `mcp:*` -> `mcp`
- `mcp:*` -> `external_mcp` (differentiator between MCPs hosted by us and external ones)
- `skill:*` -> `skill` (the `resource_id` is the project UUID)
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
starting with a base grant set and subtracting an exclusion set:

```text
effective result = base - exclusion
```

This is the same shape as a Zanzibar-style userset difference such as:

```text
viewer = allowed - blocked
```

In Speakeasy, the main example today is risk policy evaluation:

```text
risk_policy_applies =
  risk_policy:evaluate(policy_id)
  - risk_policy:bypass(policy_id, runtime_dimensions)
```

Read that as:

> Apply the policy if the user can evaluate the policy, unless the user also has
> a bypass grant for this exact policy and this exact runtime target.

The most important rule: **exclusion grants do not create access by themselves**.
They only subtract from something that the base side already proved.

`risk_policy:block` is the inverse rule store for allow-by-default (allow_all)
shadow MCP blocking policies: each grant names a policy and a `server_url`
selector and is always held by the all-users principal (`user:all`), meaning
"this server is blocked for everyone in the project". The shadow MCP hook path
reads these grants directly; they are never evaluated through `Require` and do
not participate in grant expressions.

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

The exclusion grant is real, but it is for a different concrete permission instance.
It does not subtract the `https://bcd.com` decision.

### MCP Example

The same model can express "allow broad access except a narrow blocklisted target."
For example:

```text
mcp_tool_call_allowed =
  mcp:connect(toolset_id, tool)
  - mcp:blocked_connect(toolset_id, tool)
```

Assume the user has:

```text
1. mcp:connect
   selector: {resource_kind: "mcp", resource_id: "toolset_123"}

2. mcp:blocked_connect
   selector: {
     resource_kind: "mcp",
     resource_id: "toolset_123",
     tool: "delete_database"
   }
```

| Tool call being evaluated             | Blocklist grant matches?                               | `mcp:connect - mcp:blocked_connect` result                                             |
| ------------------------------------- | ------------------------------------------------------ | -------------------------------------------------------------------------------------- |
| `toolset_123`, `tool=search_docs`     | No. The blocklist grant is only for `delete_database`. | Tool call is allowed.                                                                  |
| `toolset_123`, `tool=delete_database` | Yes. Same toolset and same tool.                       | Tool call is not allowed. The blocklist grant subtracts the exact tool-call instance.  |
| `toolset_456`, `tool=delete_database` | No. The blocklist grant is for `toolset_123`.          | Tool call is not allowed unless another base `mcp:connect` grant covers `toolset_456`. |

Again, the exclusion side only subtracts. A blocklist/bypass grant without a
matching base grant never grants anything.

### Principal Precedence

A caller's grants come from principals of different specificity: `user:all`
(everyone), the roles they hold, and the user or agent itself. Allows from all
of them combine, and a blocklist exclusion (`*:blocked_*`) from any of them
normally subtracts. One case is decided by specificity instead:

> A grant made directly to the user that names a concrete resource outranks a
> blocklist exclusion that reaches them only through a role or `user:all`.

This lets an administrator block a server for a role, including a
directory-synced one, and still give individual members of that role access to
it by name, without changing role membership.

| Grants held                                                                         | `mcp:connect` on `server_1` |
| ----------------------------------------------------------------------------------- | --------------------------- |
| role: `mcp:connect *`, `mcp:blocked_connect server_1`                               | Denied                      |
| role: `mcp:connect *`, `mcp:blocked_connect server_1`; user: `mcp:connect server_1` | Allowed                     |
| `user:all`: `mcp:blocked_connect server_1`; user: `mcp:connect server_1`            | Allowed                     |
| role: `mcp:blocked_connect server_1`; user: `mcp:connect *`                         | Denied                      |
| user: `mcp:connect server_1`, `mcp:blocked_connect server_1`                        | Denied                      |
| `user:all`: `mcp:blocked_connect server_1`; role: `mcp:connect server_1`            | Denied                      |

The rules behind that table:

- The user's own blocks always apply, including to their own grants.
- A direct grant spanning every resource (`resource_id: "*"`) does not
  outrank, so a broad user grant can still be revoked on one resource by a
  role or organization-wide block.
- Agent grants never outrank. An agent's owner can write its policy
  (`agents.createPolicyGrant`) without being an administrator, so letting those
  grants outrank would let owners lift a block an administrator put on the
  agent's role or on `user:all`.
- Roles and `user:all` share one level: a role grant does not outrank a
  `user:all` block, and the reverse. The built-in member role grants
  `mcp:connect` on every server, so letting roles outrank `user:all` would
  silently lift every "everyone: no access" rule for ordinary members.
- A narrowed direct grant outranks only as far as it reaches. A direct grant for
  one tool proves server-level access and that tool; every other tool stays
  blocked.
- Non-blocklist exclusions are unaffected. `risk_policy:bypass` exempts a
  principal from a policy rather than taking access away, so a direct
  `risk_policy:evaluate` grant never cancels it.
- Each admitted policy (credential, agent, owner) is evaluated on its own
  grants, and every policy must still allow.

The engine implements this in `server/internal/authz/precedence.go`. The same
rule is mirrored in `ListAccessibleMCPServersForUser` and
`ListAccessibleSkillsForUser` (`server/internal/access/queries.sql`),
`runtimepolicy.DelegationContained`, and the dashboard's `useRBAC` and
per-server access list. `access.listGrants` reports each scope's
`direct_selectors` so clients can apply it.

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
role:global:00000000-0000-0000-0000-000000000001 has project:write on every project
role:global:00000000-0000-0000-0000-000000000002 has mcp:connect on every MCP server
role:organization:00000000-0000-0000-0000-000000000003 has mcp:connect on toolset_123, tool=search_docs
agent:00000000-0000-0000-0000-000000000004 has mcp:connect on toolset_123
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

The important columns mirror the authorization model. `organization_id` keeps every grant inside one organization. `principal_urn` identifies the user, role, or agent receiving the grant, and `principal_type` is generated from that URN prefix. `scope` stores the permission being granted, while `selectors` stores the JSONB constraints that describe where the grant applies. `drop_resource` is deprecated and scheduled for removal.

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
role:global:00000000-0000-0000-0000-000000000002
```

The engine then evaluates checks in memory against the loaded grants.

## System Roles

Speakeasy ships with two system roles, `admin` and `member`, and their grants are defined in `authz.SystemRoleGrants`.

`admin` receives all standard scopes:

```text
org:admin
org:read
project:read
project:write
assistant:read
assistant:write
mcp:read
mcp:write
mcp:connect
environment:read
environment:write
skill:read
skill:write
plugin:write
```

`member` receives read and connect access by default:

```text
org:read
project:read
assistant:read
mcp:read
mcp:connect
skill:read
```

`member` intentionally does not receive `environment:read` (environment values include secrets, so viewing them must be granted explicitly through a custom role; admins keep `environment:read`/`environment:write`). Skills use an independent project-selectable scope family: members receive `skill:read`, while admins receive both `skill:read` and `skill:write`; `project:*` does not imply either skill scope. These Skills defaults are provisioned when the org-level `skills` product feature is enabled. Repeated provisioning intentionally does not restore customized or removed system-role grants. Disabling Skills does not remove grants. Most Observe pages remain `org:admin`-only, while the Identities roster uses the member role's default `project:read` grant; members also receive `org:read`, which opens identity detail pages.

`plugin:write` is a separate project-selected capability for plugin CRUD, skill and MCP references, and publishing. It does not imply `skill:write`, `mcp:write`, or any read scope; skill authoring permissions and defaults are unchanged. Skill distribution additionally requires `skill:read` for the referenced skill. Full plugin reads retain `org:read` (and normal project admission); minimal skill distribution discovery retains `skill:read`. Audience assignments and marketplace settings remain `org:admin`-only. Admin defaults include `plugin:write`; existing `org:admin` grants remain an explicit alternative, without rewriting customized roles. `plugin:blocked_write` excludes plugin mutations even when an admin alternative is available. Members do not receive plugin write access by default.

### Assistant scopes

`assistant:read` lets a caller list, open, and talk to assistants (including the Project Assistant and stopping a turn it started). `assistant:write` lets a caller create, update, delete, and provision them, and implies `assistant:read`. `project:*` does not imply either; assistant triggers stay on `project:read`/`project:write`. `assistant:blocked_read` and `assistant:blocked_write` store exceptions.

Assistant checks follow the MCP pattern: the check names the assistant and carries the project as a `project_id` dimension (`authz.AssistantCheck(scope, assistantID, projectID)`). Project-level operations (creating an assistant, provisioning the Project Assistant) name the project itself as the resource. List results are filtered per assistant with `authz.Engine.Filter`. Grant selectors therefore take one of three shapes:

```json
{ "resource_kind": "assistant", "resource_id": "*" }
{ "resource_kind": "assistant", "resource_id": "*", "project_id": "<PROJECT_ID>" }
{ "resource_kind": "assistant", "resource_id": "<ASSISTANT_ID>" }
```

The first covers every assistant in the organization, the second every assistant in one project (including creating new ones there), and the third exactly one assistant. A grant naming one assistant cannot create assistants or see its siblings. Attaching a toolset or MCP server to an assistant (on create or update) additionally requires `mcp:connect` on each attached server, so an assistant grant cannot widen what the assistant reaches beyond what the caller can connect to. The same selectors are valid in agent policy grants: `assistant:read` and `assistant:write` are agent-runtime-safe from runtime scope registry version 3, so an agent can hold `assistant:write` restricted to its own assistant. The role editor offers the organization-wide and per-project shapes; grants naming one assistant are created through the API and kept as stored.

Admin defaults include `assistant:read` and `assistant:write`; Member defaults include `assistant:read`. Organizations whose system-role grants were seeded before these scopes existed have no stored rows for them, so `authz.LoadGrants` supplies the unrestricted defaults for the built-in Admin and Member role principals at load time.

System roles are seeded when an organization is provisioned, and the first user is assigned the Admin role. WorkOS organization reconciliation also seeds the defaults as an idempotent backstop. System roles are not meant to be edited like custom roles. Changing their default grants is a product behavior change and should be treated carefully, especially for existing organizations.

## Dashboard Grant Reference

Embedded onboarding configuration requires `org:admin` independently of task assignment. Distributing servers additionally requires `project:write` and `mcp:write`; Anthropic observability requires `project:read`. These gates target the SDK request project (the route project, then explicit `projectSlug` query parameter, then `default`), not the preferred project in the sidebar. Missing projects or grants show a handoff message without mounting protected configuration queries. Entitlement checks remain unchanged.

Use this table when answering "what grant is required to use this dashboard feature?" It records the dashboard's page and action gates, but server-side checks remain authoritative. When a row lists multiple scopes separated by `OR`, any one of those grants can open the surface. Scope expansion still applies: `org:admin` implies `org:read`, `project:write` implies `project:read`, `assistant:write` implies `assistant:read`, `mcp:write` implies `mcp:read` and `mcp:connect`, `environment:write` implies `environment:read`, `skill:write` implies `skill:read`, and `workload:write` implies `workload:read`.

Selectors matter. A project-scoped or assistant feature needs the grant selector to match the active project. An MCP feature needs the selector to match the target MCP server or toolset. An unrestricted selector for the scope family covers every resource in that family.

| Dashboard feature or question                                                                                       | Required grant(s)                                                                                            | Selector target                                                                     | Notes                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| ------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Open organization home and see project entry points                                                                 | `org:read` OR `project:read` OR `org:admin`                                                                  | Org or project selector, depending on grant                                         | The page can render for org readers, project readers, or admins. Project visibility still depends on backend filtering and loaded organization data.                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| Create a project                                                                                                    | `org:admin`                                                                                                  | Organization ID                                                                     | The `/rpc/projects.create` handler requires `org:admin`. `project:write` is not enough because the project does not exist yet.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| Invite members, change member roles, create/update/delete roles                                                     | `org:admin`                                                                                                  | Organization ID                                                                     | Access management mutations are organization administration.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| Open Access / RBAC page                                                                                             | `org:read` OR `org:admin`                                                                                    | Organization ID                                                                     | Viewing access state can be read-only; role, grant, member-role, and challenge-resolution actions are `org:admin`. Granting a challenge adds the selected matching custom role without replacing the member's existing roles, then records the challenge resolution in the same local transaction. System-role assignment remains in member management.                                                                                                                                                                                                                                                             |
| Open Team page and manage members                                                                                   | `org:admin`                                                                                                  | Organization ID                                                                     | The current Team page is page-gated on `org:admin`.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| Open Billing                                                                                                        | `org:read` OR `org:admin`                                                                                    | Organization ID                                                                     | Billing management sections and portal actions are `org:admin`.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| See billing status banners (failed payment, inference cap reached)                                                  | `org:read` OR `org:admin`                                                                                    | Organization ID                                                                     | Per-key inference-cap banners ride the page header, so they appear on every organization and project page except Billing, where the failed-payment banner and cap banners are composed together. Stripe subscription state and per-key inference caps are read only after a pay-as-you-go tier check and an `org:read` gate. Their actions require `org:admin`; other readers see an ask-an-admin note instead.                                                                                                                                                                                                     |
| Read per-key inference usage and monthly caps (`usage.getInferenceSpendCaps`)                                       | `org:read` OR `org:admin`                                                                                    | Organization ID                                                                     | Enforced server-side against the active organization. The endpoint enumerates only the materialized Speakeasy-managed Security and Other inference keys; BYOK provider keys are excluded. The billing page's inference cap controls read it. Setting one selected key's cap (`usage.setSpendCap`) is `org:admin`.                                                                                                                                                                                                                                                                                                   |
| Manage organization API keys or generate agent tokens                                                               | `org:admin`                                                                                                  | Organization ID                                                                     | API key listing and creation are admin-only in the dashboard.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| View organization domains, org logs, webhooks, identity, audit logs, device agent                                   | `org:read` OR `org:admin`                                                                                    | Organization ID                                                                     | Mutating settings on these pages, including domains, forwarding, webhooks, identity providers, and device-agent token generation, requires `org:admin`. The Device Agent Setup tab stays readable for org readers, but the fleet Configuration tab is admin-only to both view and change: its Configuration tab is hidden from non-admins and `agent.getConfiguration`/`agent.updateConfiguration` both require `org:admin`.                                                                                                                                                                                        |
| View MCP Connections and automatic session refresh policy                                                           | `org:read` OR `org:admin`                                                                                    | Organization ID                                                                     | Changing the organization-wide automatic session refresh policy requires `org:admin`. Revoking connections requires `project:write` for the selected project.                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| View organization collections                                                                                       | `org:read` OR `org:admin`                                                                                    | Organization ID                                                                     | Collection create/update/delete and MCP server attach/detach actions require `org:admin`. Adding collection content to a project can additionally require `project:write` for that project.                                                                                                                                                                                                                                                                                                                                                                                                                         |
| Open project home                                                                                                   | `project:read`                                                                                               | Project ID                                                                          | `project:write` also opens it through scope expansion.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| Open Sources and source detail pages                                                                                | `project:read` OR `project:write`                                                                            | Project ID                                                                          | Creating, importing, reconnecting, deleting, or editing sources requires `project:write`. Creating custom remote MCP servers is gated by `mcp:write`.                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| Add OpenAPI, add Function, or add from Catalog through Sources                                                      | `project:write`                                                                                              | Project ID                                                                          | These routes create project-owned resources.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| Open Catalog                                                                                                        | `project:read` OR `mcp:write`                                                                                | Project ID or MCP selector, depending on entry point                                | Browsing from the main nav can be available to project readers. Adding a catalog server to a project is a write action and is gated at the add flow. User Identity linking requires `mcp:write` on the target MCP server and on every other MCP server in the project that shares its user session issuer; only creating a provider or OAuth client additionally requires `project:write`.                                                                                                                                                                                                                          |
| Open Playground                                                                                                     | `mcp:connect` OR `mcp:read` OR `mcp:write`                                                                   | MCP server or toolset ID                                                            | Runtime calls should use `mcp:connect`; editing saved playground/server configuration requires `mcp:write`.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| Open MCP server/toolset pages                                                                                       | `mcp:read` OR `mcp:write`                                                                                    | MCP server or toolset ID                                                            | Creating, editing, deleting, publishing, authentication, team-access, prompt/resource, OAuth, tool filtering, and settings actions generally require `mcp:write`; collection publishing can require `org:admin`. Authentication settings pass the selected MCP resource when listing attachable user session issuers, so exact resource grants and project-scoped grants authorize the same settings flow.                                                                                                                                                                                                          |
| View MCP team access                                                                                                | `mcp:read` AND `org:read`                                                                                    | MCP server or toolset ID; active organization                                       | Team access reads the organization member and role lists in addition to the MCP resource. Check access (`access.explainResourceAccess`) uses the same gate to explain any member's access and accepts a session only, not API keys; which directory role mapping gave them a role is shown only with `org:admin`.                                                                                                                                                                                                                                                                                                   |
| Create a custom remote MCP server                                                                                   | `mcp:write`                                                                                                  | MCP selector, usually unrestricted or target server ID after creation               | The create route is page-gated on `mcp:write`. Project-scoped `mcp:write` also permits listing attachable user session issuers for the creation form.                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| Open Deployments                                                                                                    | `project:read` OR `project:write`                                                                            | Project ID                                                                          | Deployment-triggering and failed-source retry actions require `project:write`.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| Open Assistants and the assistant chat pages                                                                        | `assistant:read`                                                                                             | Any assistant in the active project                                                 | Lists only the assistants the caller holds `assistant:read` for. Creating an assistant requires a project-wide `assistant:write`; editing, deleting, status, and instructions require `assistant:write` on that assistant; attaching MCP servers additionally requires `mcp:connect` on each attached server. Attaching skills additionally requires `skill:read` and `project:write`. The Triggers tab requires `project:write`; the Activity tab requires `org:read`.                                                                                                                                             |
| Use the Project Assistant (dock, home widget, command palette, Explore with AI)                                     | `assistant:read`                                                                                             | Any assistant in the active project; the server checks the Project Assistant itself | Provisioning a missing Project Assistant requires a project-wide `assistant:write`; without it the dock shows the ask-an-admin notice.                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| Open Skills / CLIs                                                                                                  | `org:read` to load the entitlement, plus `project:read` when Skills is disabled or `skill:read` when enabled | Organization ID and active project ID                                               | The org-level `skills` product feature controls both the page and navigation gate. Standard Admin and Member roles already include `org:read`. Loading or failed feature checks degrade to the disabled Coming Soon surface and its `project:read` gate. Enabled Skills checks use the active project as the `skill:read` selector. Enabling Skills provisions wildcard `skill:read` for Member and wildcard `skill:read`/`skill:write` for Admin when RBAC is already active; disabling Skills leaves those grants intact.                                                                                         |
| Open Plugins                                                                                                        | `project:read` OR `project:write`                                                                            | Project ID                                                                          | Page admission is separate from actions: full plugin reads require `org:read`; plugin CRUD, contents/references, and publishing require project-scoped `plugin:write` or the existing `org:admin` alternative. `project:write` does not grant plugin mutations. Skill distribution also requires `skill:read`; assignments and marketplace settings remain `org:admin`-only. Rotating the Observability plugin credential (`plugins.rotateObservabilityCredential`) is `org:admin`, matching the download path that also mints the hooks-scoped ingest key. `plugin:blocked_write` overrides either mutation grant. |
| Open Environments list                                                                                              | `project:read` OR `project:write`                                                                            | Project ID                                                                          | Creating an environment requires `environment:write` (project-scoped); environment card clone actions are `environment:write`.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| Open an Environment detail page                                                                                     | `project:read`                                                                                               | Project ID                                                                          | Adding, editing, or deleting variables requires `environment:write` (the environment scope family is independent from `project:*` because env values include secrets). The "Fill for MCP Server" action also requires `environment:write`: it prefills placeholder variables through the environment update path. Environment-specific clone checks use `environment:read` for the source and `environment:write` for the destination.                                                                                                                                                                              |
| Open Observe pages: Costs, MCP & Tools Insights, Agent Sessions                                                     | `org:admin`                                                                                                  | Organization ID                                                                     | The Observe nav group and command-palette entries stay visible to project members; opening one of these pages as a non-admin renders the page-level "Access restricted" notice.                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| Open Tool Logs                                                                                                      | `log:read`                                                                                                   | Active project ID                                                                   | Not a default for any system role, and not implied by `project:*` or `org:admin`; grant it per project through a custom role. The server enforces it on `telemetry.searchLogs`, `telemetry.searchToolCalls`, `telemetry.listToolUsageTraces`, and `telemetry.listHooksTraces`. Without it the page renders the "Access restricted" notice with a request-access action. The Tool Logs nav entry and the links into it (Insights, MCP server overview, gateway activity, Triggers, identity Usage, project guide) are hidden without it.                                                                             |
| Show logs in the Playground, or open a session's Tool calls view                                                    | `log:read`                                                                                                   | Active project ID                                                                   | The Playground "Show Logs" button is disabled with a tooltip without it. In a session detail panel the Tool calls entry and count are hidden and the logs request is never sent; the transcript itself still follows `chat:read` (or owner).                                                                                                                                                                                                                                                                                                                                                                        |
| List individual tool calls through Platform MCP (`search_tool_calls`, `list_recent_tool_calls`, `query_mcp_traces`) | `log:read`                                                                                                   | Target project ID                                                                   | Member tools discovered only by callers holding `log:read` somewhere; each call re-checks `log:read` on the named project. `query_mcp_traces` additionally requires visibility of the MCP server. Managed assistant calls also require live `org:admin`, as every assistant platform tool does.                                                                                                                                                                                                                                                                                                                     |
| Open the Identities roster (the Employee Enrollment replacement)                                                    | `project:read`                                                                                               | Active project ID                                                                   | The roster reads project telemetry and remains project-scoped.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| Open Agent Identity management                                                                                      | Active organization membership; no navigation scope gate                                                     | Organization / owned agent                                                          | The agent-management rollout must be enabled. The API authorizes ownership independently of RBAC and filters the inventory accordingly.                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| View the Access Hub: trusted platforms, the machines allowed under each, assigned agents                            | `workload:read` OR `workload:write`                                                                          | Unrestricted (organization-wide)                                                    | The trust policy is configured for the organization as a whole, so the grant names no individual resource. Not a member default: `workload:read` alone discloses which machines the organization recognises. The organization sidebar shows the Access Hub (under Secure) behind the same gate.                                                                                                                                                                                                                                                                                                                     |
| Trust, edit or stop trusting a platform; allow, edit, withdraw a machine; set its agent                             | `workload:write`                                                                                             | Unrestricted (organization-wide)                                                    | Allowing a machine is itself the grant of its access, and assigning an agent hands a machine that agent's authority without requiring anything on the agent (decided on AIM-291). An admin system-role default; members need an explicit grant through a custom role. Both tiers share one agent assignment for a subject, so withdrawing a machine removes it only once no admission at either tier still names that subject.                                                                                                                                                                                      |
| Open MCP Sessions                                                                                                   | `project:read`                                                                                               | Active project ID                                                                   | The sessions rollout must be enabled. Listing and facets check the project; revocation requires `project:write` on that project.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| Open an identity detail page                                                                                        | `org:read`                                                                                                   | Organization ID                                                                     | Identity resolution includes organization directory attributes and linked account identifiers. Registered-agent links redirect owners without `org:read` to Agent Identity management. Individual panels apply additional project or admin checks.                                                                                                                                                                                                                                                                                                                                                                  |
| Open an identity's Findings page (listed under Security)                                                            | `org:admin`                                                                                                  | Organization ID; each finding's chat for `chat:read`                                | Same rule as Watchdog evidence cards. The nav entry is hidden without `org:admin`, and `risk.listResults` enforces it on the server. Per row, the clickable session title, the Agent Sessions link, and expanding the row to the flagged message need `chat:read` on that finding's chat; without it the title is plain text and the chat is never requested. Revealing a masked match also needs `chat:read` and is audited.                                                                                                                                                                                       |
| Read one employee's Shadow AI detections                                                                            | `project:read`                                                                                               | Active project ID                                                                   | The server validates that the active project belongs to the active organization. The endpoint requires an employee email, canonical-folds linked aliases, and cannot be called without an employee identifier to enumerate organization-wide detections.                                                                                                                                                                                                                                                                                                                                                            |
| Change a gateway discovery default                                                                                  | `mcp:write`                                                                                                  | Project ID                                                                          | Settings follows the existing gateway update permission. Inspect mode overrides require `mcp:connect` on the gateway and do not change the owner default.                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| See a gateway's client admission setting in Debug                                                                   | `project:read` and `mcp:read`                                                                                | Project ID; gateway ID                                                              | Debug needs `project:read`, as do the sign-in server and custom URL reads behind the section; a refused or failed read shows the setting as unknown. The Settings link offers a change only with `project:write` on a project-owned sign-in server, otherwise a view. Platform MCP `get_gateway_client_admission` needs `project:read` on the exact project and `mcp:read` on the gateway (hidden as not found without it), not `log:read`.                                                                                                                                                                         |
| Open a Shadow AI tool onto the users it was detected for                                                            | `org:admin`                                                                                                  | Organization ID                                                                     | The read behind the page names people across the organization, so it is gated at the page level like the Shadow AI inventory tabs it is opened from. The page for a local model offers no access decision, since a local model never reaches the gateway.                                                                                                                                                                                                                                                                                                                                                           |
| Open or summarize an individual agent session transcript (`chat.load`, `chat.summarize`)                            | `chat:read` (or owner)                                                                                       | Chat session                                                                        | Reaching the Agent Sessions list first requires `org:admin` (row above). Anyone can always open sessions they own (owner-bypass: `chat.user_id == caller`), with no `chat:read` grant. Opening another user's session requires an explicit unrestricted `chat:read` — not a default of any system role, so even admins must be granted it. Both are recorded in the audit log (`chat_session:access`), including a cached summary.                                                                                                                                                                                  |
| Search chats or read message context through Platform MCP (`search_chats`, `read_chat`)                             | Explicit unrestricted `chat:read` (`chat:write` also satisfies it)                                           | Exact organization and project; chat and optional message                           | External authenticated users only. Admin alone does not grant transcript access. All organization-captured account types are included. Results are sanitized and chat access is audited.                                                                                                                                                                                                                                                                                                                                                                                                                            |
| Confirm traffic on the Set up Anthropic observability card                                                          | `org:admin` AND (`chat:read` OR own the delivered conversation)                                              | Chat session                                                                        | The setup card itself is page-gated on `org:admin`. The step polls `chat.listChats`, which degrades to the caller's own sessions without `chat:read`. A hook-delivered conversation is attributed to the email Claude reports, so an admin whose Claude account differs from their Speakeasy login sees nothing until they hold `chat:read` — which the Session Auditor role in the row below exists to give them, temporarily.                                                                                                                                                                                     |
| Create the Session Auditor role, or add and remove your own membership during setup                                 | `org:admin`                                                                                                  | Organization ID                                                                     | Uses `access.createRole` and `access.updateMemberRoles`, so every write is audited. Creating the role with the caller already on it emits `access_role:create` alone — `assignMembersToRoleTx` runs inside role creation and logs no event of its own; assigning a role that already exists, and the later removal, each emit `access_member:update_role`. Directory-synced organizations create the role but cannot self-assign: membership is mapped from a directory group in Identity → SCIM → Configure, because a local assignment is replaced on the next membership reconciliation.                         |
| Pin or unpin an agent session (`chat.setPinned`)                                                                    | `chat:read` (or owner)                                                                                       | Chat session                                                                        | Pinning is a shared project bookmark, not a destructive mutation of the transcript. Anyone who can open a session can pin it. Members without `chat:read` can still pin sessions they own.                                                                                                                                                                                                                                                                                                                                                                                                                          |
| Rename, delete, or submit feedback on an agent session                                                              | `chat:write` (or owner)                                                                                      | Chat session                                                                        | Deliberately separate from `chat:read` so a session reviewer can read and pin transcripts without being able to destroy one. `chat:write` satisfies `chat:read` by expansion. Anyone can always mutate sessions they own; doing it to another user's session needs an explicit unrestricted `chat:write`, which no system role holds. Deleting a chat that backs a live assistant thread is refused regardless.                                                                                                                                                                                                     |
| Open Project Settings                                                                                               | `project:write`                                                                                              | Project ID                                                                          | Settings are treated as project mutation/admin surface.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| View mapped Slack accounts on a person’s Accounts & devices page                                                    | `org:read` to open the identity page; exact caller ID or `org:admin` for the Slack read                      | Organization ID and exact Speakeasy user ID                                         | Requires a browser session and active caller/subject organization memberships. Shared Explore Demo waives only the caller membership check. No email fallback. Employees see only their own mappings, with contact-admin guidance. Admin links open the member table filtered to the membership; corrections require `org:admin`. Inactive people are reviewed through the org table. Mappings grant no extra permissions.                                                                                                                                                                                          |
| Open Slack workspaces under Organization Identity                                                                   | `org:admin`                                                                                                  | Organization ID                                                                     | List, begin, callback, disconnect, sync, member reads and mapping decisions independently enforce `org:admin`. Mapping targets must be active same-organization people; mappings grant no permissions. Only browser sessions are accepted; support sessions and API keys cannot manage connections. The shared demo can list connections but cannot begin or complete authorization, disconnect, sync or change mappings.                                                                                                                                                                                           |
| Open Secure pages: Risk Overview, Risk Policies, Approval Requests, Detection Rules                                 | `org:admin`                                                                                                  | Organization ID                                                                     | The dashboard pages are admin-only today. `risk_policy:evaluate` and `risk_policy:bypass` are policy runtime/request-flow scopes, not general dashboard page grants.                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| Open Watchdog                                                                                                       | `org:admin`                                                                                                  | Organization ID                                                                     | Both navigation and page admission require organization admin access. The risk signals and analysis status APIs enforce the same scope.                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| Reveal a finding's plaintext match in Risk Events, Risk Overview, or Watchdog                                       | `chat:read`                                                                                                  | Attributed chat ID; unrestricted for MCP findings with an empty or invalid chat ID  | Page admission remains `org:admin`, but reveal authorization follows the finding's chat attribution. Successful plaintext reveals are audited.                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| Reveal an MCP finding's full scanned tool-call payload in the risk event drawer                                     | `chat:read`                                                                                                  | Unrestricted                                                                        | The Risk Events page requires `org:admin`; the API itself requires unrestricted `chat:read`, since the payload can hold content from any chat or none. Successful payload reveals are audited.                                                                                                                                                                                                                                                                                                                                                                                                                      |

| Open Signals intelligence | `project:read` | Active project ID | Requires the organization's `signals_intelligence` product feature. Disabled or unavailable entitlement checks hide sidebar/command-palette navigation and prevent feature data from mounting; a disabled direct URL redirects to the project overview. |
| Query signal trends, discover reading attributes, or inspect a measured definition | `project:read` | Exact project ID | Analytics RPC uses the active project; Platform MCP authorizes the explicitly named project. Both require `signals_intelligence`, independently of the Explore rollout. Results contain aggregate measurements and sensor-definition context; conversation content retains chat access checks. |
| Analyze cross-session signal patterns and supporting references | `project:read`; underlying chat access for each evidence reference | Exact project and conversation | Both analytics RPC and `find_signal_sessions` use the same aggregate contract. Evidence is a bounded sample filtered through the transcript service's owner/grant, external-user, managed-runtime and support-impersonation checks. OAuth-authenticated Platform members use the same owner/grant boundary as dashboard members. Managed assistants also require live `org:admin` and retain managed-runtime chat checks. Opening a transcript checks access again. |
| Create, edit, or delete signals and sensors | `project:write` | Active project ID | Read-only users can inspect configuration. Mutation controls and submit handlers require write access. The API calls `Require` with the same project scope, subject to `ShouldEnforce`: legacy API-key authentication bypasses grant enforcement and retains producer-key/project authorization. All ten management methods independently require the organization's `signals_intelligence` product feature, including API-key calls. Disabled/missing features return forbidden; lookup errors fail closed. |

## Enforcing RBAC in Code

### Request preparation

Before handlers run, the auth middleware prepares an auth context. RBAC grant loading is handled by `authz.Engine.PrepareContext`. For an eligible request, the engine reads the auth context, builds principal URNs for the user and their organization role, loads the matching rows from `principal_grants`, and stores those grants back on the request context.

Handlers do not query grants directly. Their job is to describe the access they need and ask the engine to enforce it.

### When RBAC is enforced

`authz.Engine.ShouldEnforce` decides whether checks should actually be applied. RBAC is always enforced for authenticated requests, regardless of account tier, as long as the request is not using an API key. The request also needs a session, except for assistant-token requests, which are allowed through this path.

Scope overrides are a special case. In local development, authenticated users can use override headers. In production, only platform admins can. When valid overrides are present, RBAC is enforced so the overridden grant set is what the request experiences.

RBAC is not currently enforced for API key requests. Unauthenticated contexts are handled as authorization errors by the normal auth path. This may change as the RBAC model expands, especially when API keys move into RBAC.

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

#### Auth client checks

`mcp:auth` and `mcp:blocked_auth` protect the `mcp_auth_client` resource kind: the upstream identity Speakeasy presents to an MCP server. They have no server dimension, so a block on a shared client applies on every server that uses it. A caller will need `mcp:connect` on the server and `mcp:auth` on the selected client; neither implies the other, except that a delegated policy older than version 5 reads its `mcp:connect` grants as an auth client ceiling (below). No request is enforced against `mcp:auth` yet, and no system role holds it. `Require` denies every auth-client check where the engine does not enforce RBAC, such as for legacy API keys; the mandatory check evaluates them against their own policy (below).

Auth client checks always match strictly, for allows and blocks: a grant key the check does not carry never matches. Build them with `authz.MCPAuthClientCheck`. Every check carries `client_type` and `project_id` (the project of the server using the client); `remote_session` checks also carry `connection_owner` and `upstream_client` (the `remote_session_clients` id the connection uses), and no other type may. Evaluation rejects a check missing any of these. Checks never carry a wildcard, but grant selectors may: a grant may set `upstream_client` with `client_type` omitted, `*`, or `remote_session`, and is rejected with any other concrete type.

`upstream_client` identifies an upstream OAuth client. Combined with `connection_owner` it names one logical connection, one person's connection to that upstream client, across every auth client that shares it. Without an owner it applies to every person's `remote_session` connection through that upstream client. It is optional: a `connection_owner` grant without it applies across every upstream client, and adding it narrows the grant to one.

`connection_owner` accepts a user ID, `*`, or `$represented_user`. The engine binds `$represented_user` once per request from trusted context: the signed-in user, or the user an agent credential acts for, never the agent owner, and never an API key's or assistant token's owner. Embedded chat sessions bind no represented user, so they cannot use a `remote_session` connection. A `remote_session` check is denied unless its owner is that represented user, whatever grants match, so an allow never lets anyone use another user's connection. An agent acting for a user must also pass that user's own policy.

Agent managers restrict an agent's auth client use with `mcp:blocked_auth` grants in its policy (`agents.createPolicyGrant`). A restriction only withdraws access, so writing one needs no containment check.

A connection owner can also block one agent from their own connection without any authority over the agent (`agents.blockOnConnection`). The server writes the grant itself: principal `agent:<id>`, scope `mcp:blocked_auth`, any `remote_session` client with `upstream_client` set to the connection's upstream OAuth client and `connection_owner` set to the caller. The caller must hold a live connection through that upstream client in the organization, and can name nothing else. `agent_connection_blocks` records the owner's authority: owners list and remove only their own blocks, and agent policy edits refuse to update or delete a grant an owner recorded. If an agent manager already held the identical grant, removing the owner's block leaves that grant in place. The block names a concrete owner, so it never affects the agent acting for another user, and it pins no grant generation, so it survives refresh and reconnect.

Delegated policies keep `$represented_user` symbolic. Without a known represented user, containment accepts it only under a parent grant that is symbolic too or does not pin the owner, and intersecting it with a concrete owner yields no allow. When issuance knows the represented user, a grant pinned to that user's ID is the same owner: it contains the symbolic grant and intersects with it to `$represented_user`, while any other concrete owner yields no allow. A parent restriction pinned to a concrete owner is treated as overlapping a symbolic grant, so it must be carried, unless issuance knows the represented user and it is someone else. An inherited block that overlaps a direct grant on the represented user's connection is overridden rather than carried, as at runtime, once issuance knows the represented user; the principal's own block always remains restrictive. Without a known represented user, the block is carried, which can only narrow.

The new scopes are safe from delegated policy version 5, but credentials are issued at version 4 (`IssuedDelegatedPolicyVersion`) until every server reads version 5, so no credential carries them yet.

Policies older than version 5 are prepared in place at admission (`runtimepolicy.admitStoredPolicy`). They could not name auth clients, and under them connecting to a server meant using its upstream identity, so each `mcp:connect` grant is read with an `mcp:auth` ceiling over every client in that grant's project. A grant on one server that names no project reaches that server's project, and nothing if the server is gone. Only a grant on every server that names no project reaches the whole organization. It is only a ceiling. The live policies of the agent, owner and authorizer must still allow the client. The represented-user binding still limits personal connections to the authorizer's own, and workloads represent no user, so they reach no personal connection. Auth client checks name no server: callers pair each with an `mcp:connect` check on the server, and that pairing is what ties the ceiling to it. Stored policies, refresh and session encodings are unchanged, and nothing is re-issued. The mandatory shadow sets `legacy_auth_ceiling=true` when admission derived a ceiling, so coverage resting on it is visible. Agent sessions on personal-connection servers name no connection owner yet, so the shadow skips those checks (`no_connection_owner`). Credentials derived at admission are built at version 5 and need nothing. Every credential is issued at version 4 until `IssuedDelegatedPolicyVersion` is raised, so this path stays live until no stored row is older than version 5. Raise it only once every server reads version 5 and the agent session, workload and device agent encoders state `mcp:auth` themselves; `TestIssuedPolicyVersionWaitsForAuthClientIssuers` fails until then. Version 4 rows stay readable.

#### API key policies and the mandatory check

A legacy API key is its own principal, `api_key:<api_keys.id>`. Its policy is `mcp:connect` and `mcp:auth` grants, and their blocks, written when the key is created (`keys.create` `policy_grants`, org admin only) and deleted when it is revoked. An `mcp:auth` grant must name its client, and a key represents no user, so it never uses a personal connection. API key grants always match strictly, and no key grant matches an MCP check that lacks a project. A project-bound key's grants must name its project, and a grant on every server must name a project. A key never inherits `user:all`, its creator's roles, or a direct-user override. Audience, assignment and policy APIs neither list nor accept `api_key` principals.

`Engine.AuthorizeMandatory` evaluates `mcp:connect` and `mcp:auth` for any caller, including legacy keys and sessionless callers that `Require` skips. A legacy key is evaluated against its own live policy; every other caller against the policy its request already holds. A caller with no policy, an inactive credential, a key check without a project, or a check outside the key's own project or transport scopes is denied, and the decision says which policy it used and why it denied. `Engine.PrepareMandatory` admits the caller once and `Decide` then evaluates checks for a server's organization with no further I/O; a caller from another organization is denied with `organization_mismatch`.

#### Shadowing the mandatory check

For organizations with the `mandatory-authz-shadow` flag, MCP serving runs the mandatory check beside today's decision and records the comparison. It never changes a response. Covered: hosted and proxied server access (including denials before the RBAC check, such as a caller outside the organization), gateway members and hosted member toolsets, consent inventory, ID-JAG token issuance, and auth clients the typed resolver chose, compared with the connect-equivalent rule. An auth client used while the resolver is off is recorded as `skipped` with reason `resolver_off`, never as an allow. Not covered yet: user session minting, and auth clients on gateway members outside enforcement.

The `mcp.authz.mandatory_shadow` counter is labelled by organization, `surface`, `caller` (`anonymous`, `represented`, `unrepresented`, `cross_org`), `principal` (`anonymous`, `user`, `agent`, `workload`, `api_key`, `assistant`, `chat_session`), today's decision, and the mandatory `outcome` (`allow`, `deny`, `error`, `skipped`), `source`, `reason` and `legacy_auth_ceiling` (`true`, `false`). Disagreements and errors are also logged with the principal, resource and project ids, once per distinct disagreement every ten minutes and at debug level otherwise. Until key policies are provisioned (see below), every legacy key reads `api_key`/`no_policy`, and anonymous callers on public servers read `unauthenticated`; both are expected.

#### Default auth permissions

`mcp:auth` is mandatory, so every organization needs grants that keep today's access working:

- **Personal default.** `user:all` holds `mcp:auth` on `remote_session` clients for `$represented_user`'s own connection: everyone can use their own personal connections and no one else's. Agents get the same grant, on the agent and on roles assigned to agents, because `user:all` is not in an agent's policy. New organizations get the `user:all` grant at creation; reconciling an existing organization never adds it back. An organization whose row a login's membership sync inserts before its WorkOS creation event is not seen as new, so it gets the default from the backfill.
- **Shared clients.** Every principal that holds `mcp:connect` (or `mcp:read`/`mcp:write`) on a server using a shared client (service account, static credential, pass-through or EMA) gets `mcp:auth` naming each client linked to a server that grant reaches when the run happens, whether it names the server or is a wildcard. It never names a client type, so clients created later and clients of servers the principal cannot reach stay unauthorized, and granting server access later grants no client. These grants are recorded in `auth_permission_provisions`; while the ledger row is not taken over, grant loading marks the grant inferred, and an inferred grant never outranks an inherited block, even though it names one client. An admin writing the same grant takes it over, and it then outranks inherited blocks like any direct grant. Shared client grants wait while any recorded client or link awaits retirement, or an earlier phase failed, so a client's stable id never carries a grant into a later revival.
- **Legacy API keys.** A live key that reaches MCP and has no policy gets `mcp:connect` on every server in its project (or in every existing project, for an organization-wide key) and `mcp:auth` on those servers' shared clients, never EMA clients, since a key represents no user. A key whose live policy is entirely provisioned gets clients recorded since, in the projects it covers. A key holding any grant an admin wrote, or whose provisioned policy was removed, is left alone. Key policies wait until every auth client is recorded (the upstream OAuth migration has nothing left to record and no failed phase or gap), so none is written from an incomplete set of clients.
- **Agent attachments.** An unrevoked attachment of a live agent to its owner's connection becomes `mcp:auth` for the agent on that connection: `upstream_client` is the attached upstream OAuth client and `connection_owner` is the owner, concretely, so the grant covers every auth client sharing that upstream client and never another user's connection. The attachment keeps working until the attachment runtime is retired. An attachment the runtime would no longer serve (no user made it, the attacher no longer owns the agent, the connection is someone else's, the agent is suspended, revoked or waiting for a new owner, its owner is no longer an active member, the connection was removed or authorized again, or its sign-in, project, upstream client or issuer was removed or no longer reaches the attachment's project) or whose upstream client no personal auth client uses yet is reported for remediation by id, never carried over to another user or a broader grant.

The grants phase of the auth client backfill writes them. It runs as part of the upstream OAuth migration, and on its own for every organization through the offline `auth-permission-defaults` backfill (`server/cmd/tools/migrations/AUTH_PERMISSION_DEFAULTS_BACKFILL.md`). It only ever adds grants and covers only auth clients already recorded. Agent attachment grants name `upstream_client`, so they are written only where the organization's `auth-client-upstream-selector-writes` flag is on: the staff upstream OAuth migration, which runs in the server, writes them once it is, and the offline backfill, which has no flag provider, only reports them as waiting. Every grant it writes is recorded in `auth_permission_provisions`, and a grant recorded there is never written again, so a rerun never restores one an admin removed. An admin writing the same grant takes it over: every explicit grant write (`UpsertPrincipalGrant`, used by role, resource-audience and key policy writes, and agent policy create and update) marks the ledger row taken over afterwards, so a rollback leaves it and a rerun still never writes it again. A new explicit write path must do the same. New organizations' personal default is recorded too, as `creation_default`, which a backfill rollback leaves. A key policy is topped up only while it is exactly what was provisioned. A rollback deletes the live grants the ledger names that no admin took over, then the ledger.

Not covered here: keys, server links, agents and connect grants created after a run (write-time provisioning is a separate change before enforcement), projects created after a run for organization-wide keys, stored delegated policies on agent and workload credentials (renewed separately), demo-org sessions (which use a fixed grant set), and anonymous callers on public servers.

Before enabling enforcement: turn on `mandatory-authz-shadow`, note the `mcp.authz.mandatory_shadow` baseline (legacy keys read `api_key`/`no_policy`), run the backfill, and confirm `no_policy` and unexplained `deny` outcomes go to zero.

#### Enabling enforcement

Enforcement is per organization: it is on once staff turn it on from the admin dashboard's upstream OAuth migration page (`enableUpstreamAuthEnforcement`), which sets the organization's `upstream_auth_enforced` feature. It applies whatever the `auth-client-resolver` flags say, so a flag change never skips its readiness checks, and the generic product feature toggle cannot set it. The flags alone only shadow. If the enforcement state cannot be read, the request is refused. Under enforcement the typed auth client resolver serves the organization's direct proxied servers and gateway members, and an assistant turn's credential is served only through the execution-selection profile version it binds (a turn credential that binds none is refused); using the resolved client requires `mcp:auth` through the mandatory check, so a caller the generic check skips is still evaluated, and a check that cannot be evaluated refuses. An unmapped or ambiguous choice, one that names no auth client and is not an explicit no-auth choice, reconnects rather than falling back (an explicit no-auth choice stays anonymous), and a client the resolver hands back to the upstream client choice (stale headers, an EMA, headers on a tunneled server) is used only after the same check for that client. A caller authenticated to another organization is refused on every public serving path, ungated or issuer-gated, remote, hosted or gateway, before any upstream credential is used. A caller with no organization is served as an anonymous caller.

Turn enforcement on for an organization only after every server replica runs a release that reads `upstream_auth_enforced`: an older replica does not read the feature and keeps serving the upstream client choice. Deploying such a release changes nothing on its own; the feature defaults off and nothing sets it except the enable endpoint.

Staff can turn enforcement on only when the organization is migrated, its auth clients are recorded, its default auth permissions are granted, every credential choice that one auth client can serve names it, and its assistant profiles are recorded. Choices several clients could serve, and profile servers with no choice, do not block it: those credentials are asked to reconnect and those turns fail with an actionable error, never served with a guessed client. Staff also confirm that the organization's `mcp.authz.mandatory_shadow` shows no unexplained new denial, and that the organization was told about the one intended change: a valid credential from another organization on one of its ungated public servers is refused with an organization mismatch instead of being served anonymously, and is never retried as anonymous.

Turning enforcement off (`disableUpstreamAuthEnforcement`) is the organization's rollback to the upstream client choice; servers see it within half a minute. Choices keep being captured at issuance either way, and a configuration change made since the last run is picked up by running the migration again, which only maps what is still unmapped.

The upstream client choice, the server-level remote session issuer, issuer-level outbound client bindings, agent attachments and the gateway's copied member clients stay in place until enforcement is stable for every organization; removing them is a separate cleanup.

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
  "principal_urn": "role:organization:00000000-0000-0000-0000-000000000010",
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
  "principal_urn": "role:organization:00000000-0000-0000-0000-000000000010",
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
  "principal_urn": "role:organization:00000000-0000-0000-0000-000000000011",
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
  "principal_urn": "role:organization:00000000-0000-0000-0000-000000000012",
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
  "principal_urn": "role:organization:00000000-0000-0000-0000-000000000012",
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
    "principal_urn": "role:organization:00000000-0000-0000-0000-000000000010",
    "scope": "project:read",
    "selectors": {
      "resource_kind": "project",
      "resource_id": "project_a"
    }
  },
  {
    "principal_urn": "role:organization:00000000-0000-0000-0000-000000000010",
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

RBAC in Speakeasy is a small set of moving pieces. Scopes define what can be done. Selectors define where a grant applies. Principals receive grants. Handlers create checks. The engine compares the request's grants to the handler's checks.

Keep scopes coarse and customer-meaningful. Use selectors and dimensions for resource-specific narrowing. Put authorization checks close to the handler boundary. Use `Filter` for partial visibility. Treat every new scope as a product contract, not just a code constant.

## Resources

Please refer to the original [RFC](https://www.notion.so/speakeasyapi/RFC-Gram-RBAC-Scope-Permission-Design-319726c497cc8177b7e9dea65a91ff10) which contains the original decision making process during RBAC
planning. Please note that this might be slightly outdated, and this guide should be the actual source of truth.

### Gateway discovery configuration

Every organization can set gateway defaults and per-connection discovery choices. Gateway defaults require project `mcp:write`; Inspect overrides require gateway `mcp:connect`. The organization gateway has no gateway default (it serves Progressive); each connection can choose its mode on the sign-in consent page.

Platform MCP's `get_mcp_connection_settings` includes a gateway's discovery mode. During preview, mode changes use the dashboard or management API. The existing Platform MCP address and network mutations remain limited to those settings; an agent-facing mode mutation is deferred until it has its own confirmation and concurrency contract.
