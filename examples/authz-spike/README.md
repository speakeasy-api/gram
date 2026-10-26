# Authorization client spike: design and walkthrough

**Design reference only — do not merge.** This exploration is intended for a
non-production draft PR retained for future review, not a production implementation
or an approved migration. A production delivery must be developed and reviewed separately.

Read the [RFC](RFC.md) for the proposed design and unresolved decisions. This
README is the detailed reference-code walkthrough.

This is an isolated, in-memory exploration of a Speakeasy authorization client
with a replaceable backend. It is not wired into production, is not a completed
RBAC migration, and does not inherit implementation or test evidence from earlier
spikes. We are developing it in small slices and reviewing one file at a time.

Run the handler-style showcase from the repository root:

```sh
mise exec -- go run ./examples/authz-spike
```

No tests have been added by request. Compiling and running these examples is a
basic demonstration, not security verification or proof of production parity.

Production-shaped handler examples live separately in
[`usecases/`](usecases/README.md), organized by domain and reviewed one at a time.
They include [`skills/get.go`](usecases/skills/get.go) and the project
[`list/detail handlers`](usecases/projects/list.go), with independently runnable
blocking and project-visibility scenarios. These compile against
the authorization package but are not wired into production or the showcase.
The next domain example covers MCP
[`connection admission and per-tool invocation`](usecases/mcps/invoke.go), with a
[`runnable capture scenario`](usecases/mcps/toolaccess/main.go).

## What the architecture separates

The application asks authorization questions through `authz.Client`, while
permission definitions describe their meaning and the backend evaluates policy.
Application handlers do not select a backend or reproduce its matching rules.

| Part               | Responsibility                                                                                                                          |
| ------------------ | --------------------------------------------------------------------------------------------------------------------------------------- |
| Application/domain | Authenticate the caller, load trusted resources, choose the required permission, authorize policy mutations, and perform protected work |
| Definitions        | Describe scopes, resource families, implication, opposing scopes, permitted grant boundaries, and supported question shapes             |
| Client             | Validate questions and policy shapes, reject incompatible inputs, and expose consistent authorization APIs                              |
| Backend            | Load coherent policy and evaluate concrete operations and discovery                                                                     |

The only implemented backend is in memory. A future database or external backend
must preserve the behavioral and consistency contracts; a matching constructor
does not make providers interchangeable. Policy-write ownership and atomicity
would also need to be preserved, not just permission checks.

## Terminology

These terms distinguish the shared permission model, assigned policy, and the
question an application asks. In particular, **policy means a collection of grants
evaluated together** in this spike, not a separate rule language or a risk policy.

| Term                   | Meaning                                                                                                                      | Example                                                                                   |
| ---------------------- | ---------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------- |
| Permission / scope     | The name of an authorization capability or opposing scope                                                                    | `mcp:connect`, `mcp:blocked_connect`                                                      |
| Permission definition  | Shared rules describing a scope's resource family, implications, exclusions, grant boundaries, and supported question shapes | MCP write can satisfy MCP read; connect supports tool invocation and discovery            |
| Principal              | A tenant-bound identity that holds policy, with an explicit type and ID                                                      | User A or Agent A in an organization                                                      |
| Grant source           | The tenant-bound assignment origin retained during policy resolution                                                         | User A directly, Role Engineering, or all users                                           |
| Authentication         | How middleware authenticated that principal, independently of its type                                                       | A human session or a bound credential                                                     |
| Subject                | The principal and its authentication for the current request                                                                 | Agent A acting through Credential C                                                       |
| Resource reference     | A concrete organization, resource kind, and resource ID                                                                      | Server A in Organization O                                                                |
| Target                 | The resource being accessed plus its trusted enclosing boundaries                                                            | Server A with Project A and Organization O as ancestors                                   |
| Ancestor               | An enclosing resource supplied by the trusted resource adapter                                                               | Project A is an ancestor of Server A                                                      |
| Anchor                 | The boundary at which a grant applies                                                                                        | One server, a project, or an organization                                                 |
| Constraint             | A policy requirement narrowing a grant within its anchor                                                                     | Only tool `list`, or only tools classified `read_only`                                    |
| Tool facts             | Information about a concrete invocation, compared with grant constraints                                                     | Calling `list_documents`, classified `read_only`                                          |
| Grant                  | One assigned scope, anchor, and optional constraints; no effect field                                                        | MCP connect on Project A, restricted to read-only tools                                   |
| Policy                 | A collection of grants interpreted together, including applicable opposing grants                                            | Allow Project A's tools except `delete_document` on Server A                              |
| Assignment containment | A management-time proof that requested grants do not exceed the editor's delegable authority                                 | A read-only user cannot assign write access to an agent                                   |
| Lifecycle owner        | The user associated with an agent for management and disablement, not an additional runtime policy                           | Removing that user requires disabling their agents                                        |
| Form                   | A concrete question's structural shape, not a selector                                                                       | Existing-resource operation, tool invocation, or creation                                 |
| Selector               | The grant's reach and restrictions; represented by anchor and constraints in this spike                                      | Server A's read-only tools                                                                |
| Discovery              | Proving some effective access remains without authorizing a particular invocation                                            | Admitting a connection when at least one policy region survives exclusions and all limits |
| Decision               | An evaluation outcome with a reason and policy revision, not a reusable credential                                           | `Allowed`, `Denied`, or `Unresolved`                                                      |

### A policy is a grant collection

For example, these two grants form one policy. Here `projectA` and `serverA` are
concrete resource references in the same organization, and Server A belongs to
Project A:

```go
policy := []authz.Grant{
    {
        Permission: authz.MCPConnect,
        Anchor:     projectA,
    },
    {
        Permission:  authz.MCPBlockedConnect,
        Anchor:      serverA,
        Constraints: authz.Constraints{Tool: "delete_document"},
    },
}
```

Together they mean: permit tool use across Project A except `delete_document` on
Server A, subject to any other applicable limits. The permission definitions
describe how the scopes behave; these grants describe the assigned policy.

For an agent using a credential, the backend evaluates the agent's assigned grants.
It also evaluates the current delegator's grants independently, requiring both
policies to permit the operation. The credential binds this execution context;
it does not carry a separate grant collection.
The owner's permissions are not loaded for execution. Assignment containment and
owner lifecycle handling belong to management workflows, which remain unfinished.

## File guide

These files are easier to understand in conceptual order than implementation order:

| File                                               | Question it answers                                                                         |
| -------------------------------------------------- | ------------------------------------------------------------------------------------------- |
| [`authz/resource.go`](authz/resource.go)           | What resource is being accessed, and where does it belong?                                  |
| [`authz/permission.go`](authz/permission.go)       | What does a scope mean, and how is it granted?                                              |
| [`authz/tool.go`](authz/tool.go)                   | What do we know about this particular tool invocation?                                      |
| [`authz/mcp.go`](authz/mcp.go)                     | How are the MCP scopes declared using those generic concepts?                               |
| [`authz/organization.go`](authz/organization.go)   | How are organization read and administration separated?                                     |
| [`authz/project.go`](authz/project.go)             | How do existing-project grants differ from project creation?                                |
| [`authz/skill.go`](authz/skill.go)                 | How do skill grants cover existing resources and project-parent creation?                   |
| [`authz/environment.go`](authz/environment.go)     | How are secret-bearing environment operations isolated from project permissions?            |
| [`authz/assistant.go`](authz/assistant.go)         | How are assistant configuration and creation authorized without granting runtime authority? |
| [`authz/plugin.go`](authz/plugin.go)               | How are explicit plugin content grants separated from member execution and audiences?       |
| [`authz/chat.go`](authz/chat.go)                   | How are explicit transcript grants separated from ownership and metadata visibility?        |
| [`authz/agent.go`](authz/agent.go)                 | How are independent agent-management scopes separated from runtime identity?                |
| [`authz/subject.go`](authz/subject.go)             | Who is acting, and how did they authenticate?                                               |
| [`authz/admission.go`](authz/admission.go)         | Which asserted execution context may proceed to policy resolution?                          |
| [`authz/policy.go`](authz/policy.go)               | Where do grants originate, and which policies remain independent?                           |
| [`authz/memory_policy.go`](authz/memory_policy.go) | How are memberships and grant sources resolved under one coherent read?                     |
| [`authz/request.go`](authz/request.go)             | What does an application send to the client?                                                |
| [`authz/client.go`](authz/client.go)               | What is validated before evaluation or policy persistence?                                  |
| [`authz/evaluate.go`](authz/evaluate.go)           | How does one policy permit or exclude an operation?                                         |
| [`authz/credential.go`](authz/credential.go)       | How is a credential bound to a principal without separate permissions?                      |
| [`authz/delegation.go`](authz/delegation.go)       | How does a trusted delegator constrain an agent without consulting ownership?               |
| [`authz/alternatives.go`](authz/alternatives.go)   | How do OR checks preserve branch-local or shared exclusion semantics?                       |
| [`authz/groups.go`](authz/groups.go)               | How can an independent read path coexist with an exclusion-aware write fallback?            |
| [`authz/filter.go`](authz/filter.go)               | How is a bounded candidate collection filtered under one policy snapshot?                   |
| [`authz/memory.go`](authz/memory.go)               | How are policies loaded and replaced coherently in this demonstration?                      |
| [`main.go`](main.go)                               | What would handlers and their example requests look like?                                   |

The storage mechanics of `memory.go` are incidental, but its coherent reads and
discovery algorithm are meaningful behavior, not placeholders to discard blindly.

## Resources, ancestors, and grant anchors

### Resource identity

A `ResourceRef` identifies one concrete resource in one organization:

```go
serverRef := authz.ResourceRef{
    OrganizationID: "example-org",
    Kind:           authz.MCPServer,
    ID:             "server-a",
}
```

The kind prevents an MCP grant from matching an environment with the same ID;
the organization ID establishes the tenant boundary. References cannot have
empty identity fields or wildcard identities.

### Ancestors describe relationships

A `Target` combines the accessed resource with trusted enclosing resources:

```go
target := authz.Target{
    Resource: serverRef,
    Ancestors: []authz.ResourceRef{
        {OrganizationID: "example-org", Kind: authz.Project, ID: "project-a"},
        {OrganizationID: "example-org", Kind: authz.Organization, ID: "example-org"},
    },
}
```

This describes Server A inside Project A inside the organization. The service's
resource adapter supplies these facts from loaded data rather than request-supplied
project IDs. In the showcase, `serverRow.AuthzTarget()` owns this mapping once,
instead of every handler repeating it.

An ancestor is not an additional permission. An MCP grant anchored to Project A
can cover this server if the permission definition permits project anchors;
`project:read` does not acquire MCP-use authority just because the server belongs
to a project.

### Anchors describe the reach of a grant

A grant's `Anchor` is the boundary to which that scope was assigned:

- A server anchor covers that specific server.
- A project anchor can cover resources in that project for scopes that permit it.
- An organization anchor can cover resources across projects in that organization.

The target supplies ancestry; the grant supplies its anchor; permission definitions
say which kinds of anchors are legal. Matching still requires the appropriate scope
and constraints, not merely a shared ancestor.

### Anchor matching in practice

Consider three servers spread across two projects:

```text
Organization O
├── Project A
│   ├── Server 1
│   └── Server 2
└── Project B
    └── Server 3
```

Server 1's target contains its own reference and the Project A and Organization O
ancestors. The evaluator checks whether the anchor kind is allowed, then compares
the grant anchor with the resource or one of those ancestors using organization,
kind, and ID together. Tool constraints narrow any matching grant further.

| Anchor for an MCP-use grant | Server 1 | Server 2 | Server 3 |
| --------------------------- | -------- | -------- | -------- |
| Server 1                    | Matches  | No       | No       |
| Project A                   | Matches  | Matches  | No       |
| Organization O              | Matches  | Matches  | Matches  |

Holding any one of these unconstrained grants can permit invocation on Server 1,
provided no opposing grant or other applicable policy limit prevents it. Future
servers are covered when their trusted targets include the matching ancestor.
Matching establishes coverage, not the final allow: this is not a most-specific-
grant-wins model, and a server-specific allow does not automatically defeat a
broader opposing grant.

### Organization binding is mandatory

Every grant and target is organization-bound, but not every grant is anchored at
the organization level. A project-scoped grant still carries an organization ID:

```text
Organization: example-org
Grant anchor: Project A
Permission:   mcp:connect
```

The client enforces these structural invariants:

- Every resource reference has a concrete organization ID, resource kind, and ID.
- Every non-organization target includes exactly one organization ancestor.
- An organization reference's ID equals its organization ID.
- An organization target names that organization itself and has no ancestors.
- All target references belong to the same organization, with no repeated references.
- The authenticated subject and target belong to the same organization.
- Policy grants belong to the same organization as their policy subject.

These checks are universal, not optional requirements repeated by each permission
definition. MCP-specific project ancestry is checked separately.

`snapshotTarget` also copies the ancestor slice at the client boundary. This
protects the evaluation input from subsequent changes to the caller's slice;
it is not a database transaction or a snapshot of policy. Structural checks cannot
prove a supplied parent is truthful, so organization-scoped resource loading remains
the application's responsibility.

## Permission definitions versus actual grants

`Permission` names a scope. `PermissionDefinition` supplies the shared rules for
that scope, while `Grant` is an actual assignment stored for a principal.

| Definition field     | Meaning                                                                                 |
| -------------------- | --------------------------------------------------------------------------------------- |
| `Name`               | Stable scope name, such as `mcp:connect`                                                |
| `Resource`           | Protected resource family                                                               |
| `AllowedAnchors`     | Boundaries at which this scope may be granted                                           |
| `AllowedConstraints` | Typed grant attributes this scope permits; empty means no constraints                   |
| `ImpliedBy`          | Scopes that directly satisfy this permission; no automatic recursive expansion          |
| `ExcludedBy`         | Opposing scopes that can remove this permission                                         |
| `Forms`              | Supported concrete resource, tool, or creation questions; opposing scopes have no forms |
| `RequiredAncestors`  | Additional ancestry required for an existing-resource target                            |
| `CreationParent`     | Parent target used when the child does not yet exist                                    |
| `Discoverable`       | Whether the scope supports a separate admission/discovery question                      |

For example, the MCP connection definition permits server, project, and organization
anchors, is implied by MCP read/write, and is opposed by `mcp:blocked_connect`.
MCP blocks remain independent: blocking read does not implicitly block connect.

### Grant use of every server across an organization's projects

An organization-wide grant needs no wildcard resource ID:

```go
grant := authz.Grant{
    Permission: authz.MCPConnect,
    Anchor: authz.ResourceRef{
        OrganizationID: "example-org",
        Kind:           authz.Organization,
        ID:             "example-org",
    },
    Constraints: authz.Constraints{},
}
```

Assigned to a user, this covers servers whose targets include that organization
ancestor. It does not cross organization boundaries, grant management, or bypass
opposing scopes or other applicable permission limits. The organization-wide
anchor is the spike's representation, not a claim about current production storage.

### Narrow a project grant to one tool

The same grant structure expresses narrower policy:

```go
grant := authz.Grant{
    Permission: authz.MCPConnect,
    Anchor: authz.ResourceRef{
        OrganizationID: "example-org",
        Kind:           authz.Project,
        ID:             "project-a",
    },
    Constraints: authz.Constraints{Tool: "list"},
}
```

Both populated constraints must match: `Tool` narrows the tool name, and
`Disposition` narrows classification. Empty fields impose no constraint.
The principal is associated with the stored policy, not embedded in each grant.

### Opposing grants are scopes, not effects

To oppose connection or tool use on Server A, assign the opposing scope:

```go
grant := authz.Grant{
    Permission: authz.MCPBlockedConnect,
    Anchor: authz.ResourceRef{
        OrganizationID: "example-org",
        Kind:           authz.MCPServer,
        ID:             "server-a",
    },
    Constraints: authz.Constraints{},
}
```

There is no `Effect`, `Allow`, or `Prohibit` field. `ExcludedBy` declares the scope
relationship. The initial evaluator subtracts matching opposing grants; existing
production principal-specific exception precedence is not yet implemented here.
It must not be silently replaced by this simpler evaluator during a migration.

## MCP declarations: valid questions and required inputs

[`authz/mcp.go`](authz/mcp.go) supplies MCP scope constants and definitions, not
endpoint-specific handler methods or an evaluator. Its six scopes comprise connect,
read, and write alongside their three independent opposing scopes.

The connection definition shows how generic definition fields become concrete rules:

```go
authz.PermissionDefinition{
    Name:              authz.MCPConnect,
    Resource:          authz.MCPServer,
    AllowedAnchors:    []authz.ResourceKind{authz.MCPServer, authz.Project, authz.Organization},
    ImpliedBy:         []authz.Permission{authz.MCPRead, authz.MCPWrite},
    ExcludedBy:        []authz.Permission{authz.MCPBlockedConnect},
    Forms:             []authz.OperationForm{authz.ToolOperation},
    RequiredAncestors: []authz.ResourceKind{authz.Project},
    Discoverable:      true,
}
```

This permits three kinds of grant boundary without allowing project permissions to
imply MCP access. Read and write can directly satisfy connect, but matching blocked-
connect can subtract it. The following fields govern the question's structure;
none of them independently grants access.

### Forms: what concrete question is valid?

`Forms` describes supported input shapes, not which resources a grant selects:

| Permission    | Supported concrete forms                 |
| ------------- | ---------------------------------------- |
| `mcp:connect` | Tool invocation                          |
| `mcp:read`    | Existing-resource operation              |
| `mcp:write`   | Existing-resource operation and creation |

A concrete `mcp:connect` request must supply a tool name, although its disposition
may be unknown. Omitting `Tool` produces a resource-level question that this scope
does not support; use discovery for connection admission instead.

### Forms are not selectors

A form determines the kind of question being asked, while a selector determines
the reach of a grant. The spike splits selector responsibilities into `Anchor` for
the resource boundary and `Constraints` for tool/classification restrictions:

```go
// Policy selecting read-only tools on Server A.
grant := authz.Grant{
    Permission:  authz.MCPConnect,
    Anchor:      serverA,
    Constraints: authz.Constraints{Disposition: authz.ReadOnly},
}
```

The invocation request's form requires tool facts; evaluation matches those facts
and the target against the grant's constraints and anchor. Forms validate question
structure rather than selecting accessible resources or tools.

### Required ancestors: what relationships must be supplied?

`RequiredAncestors: []ResourceKind{Project}` requires exactly one project ancestor
on an existing MCP target. Even a user with a server-specific allow must supply
complete ancestry: omitting the project could otherwise hide a project-scoped
opposing grant. The requirement also makes project-wide allow matching consistent.

The application must obtain the relationship from trusted data. The organization
ancestor is enforced universally, so it need not be repeated in MCP definitions.

### Creation parent: what replaces a resource that does not exist yet?

MCP write supports `CreationOperation` with `CreationParent: Project`. Creation
therefore checks a project target rather than inventing a new server identifier:

```go
// projectTarget is constructed from a trusted project and its organization.
request := authz.Request{
    Permission: authz.MCPWrite,
    Target:     projectTarget,
    Creation:   true,
}
```

The client requires a creation-capable permission, a project target, and no tool
invocation facts. The evaluator then checks policy covering that parent boundary;
a grant anchored only to an existing server cannot authorize arbitrary creation.
Connect has neither a creation form nor a creation parent, so using it for creation
is an invalid question, not merely a missing-grant denial.

### Discoverable: may we ask if some usable access remains?

`Discoverable: true` enables the separate discovery API for connect. For example,
allowing all tools while blocking `delete_document` leaves usable policy, so
admission can succeed even though that invocation is refused later. Blocking every
permitted tool region instead leaves no access, so discovery refuses admission.

Opposing scope definitions have no concrete forms and are not discoverable: they
are policy inputs, not operations someone executes. These declarations also do not
yet implement restricted audiences; write-to-connect implication still applies.

## Tool facts: what do we know about an invocation?

[`authz/tool.go`](authz/tool.go) describes operation facts, not permissions.
`Disposition` is a string classification with these supported values:

| Value                | Meaning                                                               |
| -------------------- | --------------------------------------------------------------------- |
| `DispositionUnknown` | No usable classification is available; represented by an empty string |
| `ReadOnly`           | Classified as nonmutating                                             |
| `Destructive`        | Classified as destructive                                             |
| `Idempotent`         | Classified as idempotent                                              |
| `OpenWorld`          | Classified as interacting with an open world                          |

The representation currently holds one classification at a time. Real annotation
properties can overlap, so this is not a general model of every tool characteristic.

`ToolFacts` identifies the concrete tool and its classification:

```go
facts := authz.ToolFacts{
    Name:        "list_documents",
    Disposition: authz.ReadOnly,
}
```

The name describes what the caller wants to execute, while classification comes
from the application's chosen metadata source. The showcase reads classification
from stored metadata rather than a request parameter.

Facts and constraints have different roles, even when they carry similar values:

```go
// Policy requirement: only tools classified as read-only.
constraints := authz.Constraints{Disposition: authz.ReadOnly}

// Operation facts: this named invocation is classified as read-only.
facts := authz.ToolFacts{
    Name:        "list_documents",
    Disposition: authz.ReadOnly,
}
```

Evaluation compares the requirement with the available facts. Unknown disposition
cannot prove a read-only requirement, and a potentially applicable classification-
based exclusion cannot simply be ignored. An unconstrained grant does not need a
classification to match.

There is also a difference between no invocation and an unclassified invocation:

```go
// No concrete tool invocation is supplied in this request.
request := authz.Request{Permission: authz.MCPRead, Target: target, Tool: nil}

// A concrete invocation exists, but its classification is unknown.
request = authz.Request{
    Permission: authz.MCPConnect,
    Target:     target,
    Tool: &authz.ToolFacts{
        Name:        "list_documents",
        Disposition: authz.DispositionUnknown,
    },
}
```

The first request is a resource-level question, not an unclassified invocation;
permission definitions determine whether that question shape is supported.
Connection discovery uses its own API rather than fabricating tool facts.

## Connection discovery is not tool execution

A connection has no tool disposition. A concrete tool invocation has a name and
may have a known classification, so these questions need different contracts:

- **Discovery:** prove that some tool-use policy remains after exclusions and all
  applicable permission limits, without authorizing a particular invocation.
- **Execution:** prove that this exact tool invocation satisfies policy with the
  available facts.

The current discovery implementation partitions exact tool names and disposition
values to establish a nonempty policy region. It does not assert that a matching
tool exists in the current server catalog, and its result is not an execution grant.

Missing classification does not automatically mean denial. The examples distinguish:

| Policy                                                     | Connection                         | Unclassified invocation                              |
| ---------------------------------------------------------- | ---------------------------------- | ---------------------------------------------------- |
| Unconstrained MCP-use grant                                | Allowed                            | Allowed                                              |
| Read-only MCP-use grant                                    | Allowed when usable policy remains | Unresolved; `Require` refuses execution              |
| Server-wide opposing connect scope                         | Denied                             | Denied                                               |
| Opposing scope for `lookup` only, with other usable access | Allowed                            | `lookup` denied; other permitted tools remain usable |

`ToolFacts` carry observations about the invocation; `Constraints` describe policy
requirements. Provider annotations are claims, not proof of behavior, and structural
validation cannot correct a false read-only classification. The classification
source/override/reclassification lifecycle is not implemented in this spike.

## Handler ownership and API ergonomics

The calling service chooses its endpoint's required permission. The client
validates whether that permission fits the target and input shape, without owning
endpoint-specific business decisions such as who may edit an audience.

The tool handler loads the server within the authenticated organization, looks up
tool metadata, and makes one authorization call before protected execution:

```go
if err := s.authz.Require(ctx, subject, authz.Request{
    Permission: authz.MCPConnect,
    Target:     server.AuthzTarget(),
    Tool:       &authz.ToolFacts{Name: toolName, Disposition: disposition},
}); err != nil {
    return "", err
}
```

Inspection uses the same API without tool facts:

```go
err := s.authz.Require(ctx, subject, authz.Request{
    Permission: authz.MCPRead,
    Target:     server.AuthzTarget(),
})
```

Connection admission uses a separate question and checks the result explicitly:

```go
decision, err := s.authz.DiscoverAccess(
    ctx, subject, authz.MCPConnect, server.AuthzTarget(),
)
if err != nil {
    return "", err
}
if decision.Outcome != authz.Allowed {
    return "", authz.ErrDenied
}
```

`Request.Creation` selects creation at a parent boundary, and cannot be combined
with tool invocation. No creation handler has been added yet.

We replaced per-business-action builders and separate target constructors with this
struct API to avoid method proliferation and repeated error handling. Fluent methods
such as `WithTool` remain a possible shorthand, but are not implemented. We retained
the struct because it is short and explicit, and avoided `WithProjectID` setters that
would encourage handlers to reconstruct trusted ancestry.

### Why definitions are passed at startup

Startup currently supplies `authz.MCPDefinitions()` because MCP is the first runnable
resource family, not because the client is intended to be MCP-specific:

```go
client, err := authz.NewClient(authz.NewMemoryBackend(), authz.MCPDefinitions())
```

The client validates and copies the ordered declaration slice into a map once,
allowing duplicate-name detection during setup and indexed lookup during requests.
It also checks referenced scopes, operation forms, and creation parents; validation
is not yet a complete generic schema checker. Definitions passed to a backend are
copied so evaluation cannot mutate the client's stored declarations.

A complete deployment would supply definitions for all supported families at startup,
not per handler. Some remaining resource-kind constants still lack permission
definitions and use-case implementations. Supported grant attributes now come from
each definition's `AllowedConstraints`, not a resource-family check in the client.

## Principal identity and authentication are separate

[`authz/subject.go`](authz/subject.go) explicitly separates who is acting from how
they authenticated. Principal type is not inferred from an ownership row, and
credential authentication is not selected by a nonempty optional field:

```go
subject := authz.Subject{
    Principal: authz.Principal{
        OrganizationID: "example-org",
        Kind:           authz.AgentPrincipal,
        ID:             "agent-a",
    },
    Authentication: authz.Authentication{
        Kind: authz.CredentialAuthentication,
        ID:   "credential-a",
    },
    Delegation: authz.Delegation{
        Kind: authz.DelegatedExecution,
        Delegator: authz.Principal{
            OrganizationID: "example-org",
            Kind:           authz.UserPrincipal,
            ID:             "invoking-user-a",
        },
    },
}
```

The organization belongs to the principal identity so storage keys
and credential bindings all include the tenant, type, and ID without
duplicating tenant fields in the authenticated subject. A user and agent with the
same textual ID are different principals and cannot share policy accidentally.

The two independent kinds select these supported paths:

| Principal kind   | Authentication kind | Behavior                                                                                     |
| ---------------- | ------------------- | -------------------------------------------------------------------------------------------- |
| User             | Session             | Evaluate user policy; middleware must already have authenticated the session                 |
| User             | Credential          | Validate credential binding and evaluate user policy                                         |
| Agent            | Credential          | Require delegated execution; validate the agent/delegator binding and evaluate both policies |
| Agent            | Session             | Unsupported in this spike; refused rather than treated as a human session                    |
| Unknown or empty | Any                 | Refused                                                                                      |
| Any              | Unknown or empty    | Refused                                                                                      |

Both authentication paths require a concrete ID. Principal kind is explicit rather
than inferred from ownership. The backend checks credential
binding to the full principal identity, not merely a matching textual ID.
Subjects also explicitly select `DirectExecution` or `DelegatedExecution`.
Direct execution supports users only; delegated execution supports one agent and
one same-organization user delegator. Missing/unknown modes and direct-agent paths
are refused rather than silently omitting the bound.

These fields are trusted middleware output, not a way for request parameters to
choose authority. Structural validation does not authenticate a session, verify a
token, or prove human identity. The showcase's `exampleUserSession` and
`exampleAgentCredential` helpers construct synthetic middleware output only;
`typedIdentityExample` demonstrates the supported and refused combinations.

Policy writers now take `Principal`, not an authenticated `Subject`, because the
identity receiving a grant or credential does not need a fabricated session:

```go
principal := authz.Principal{
    OrganizationID: "example-org",
    Kind:           authz.UserPrincipal,
    ID:             "user-a",
}
err := client.ReplaceGrants(ctx, principal, grants)
```

This is the recipient identity, not the editor's authority. A management domain
still needs to authenticate and authorize the editor before invoking persistence.
Roles, staff, workload identities, and other authentication modes are not supported
by these two principal kinds merely because production has such paths.

## Request walkthrough: the application-facing question

[`authz/request.go`](authz/request.go) describes what the handler is asking, not
the grants used to answer it. The subject is passed separately, so together the
arguments identify who wants to do what, to which resource, with which facts:

```go
err := client.Require(ctx, subject, authz.Request{
    Permission: authz.MCPConnect,
    Target:     server.AuthzTarget(),
    Tool: &authz.ToolFacts{
        Name:        "list_documents",
        Disposition: authz.ReadOnly,
    },
})
```

Each request field has one responsibility:

- **Permission:** the scope chosen by the calling service for its endpoint;
  authorization interprets that scope rather than choosing the endpoint's rule.
- **Target:** trusted resource identity and ancestry, supplied by the domain
  adapter; creation instead names the parent of the not-yet-existing resource.
- **Tool:** concrete invocation facts when non-nil; nil does not mean any tool
  is permitted.
- **Creation:** asks about creation at a parent boundary, not modification of an
  existing resource; it cannot be combined with tool facts.

The handler does not set `OperationForm` separately. The client derives it from
the request contents, then checks support in the permission definition:

| Request contents              | Derived form        |
| ----------------------------- | ------------------- |
| No tool, creation false       | `ResourceOperation` |
| Tool supplied, creation false | `ToolOperation`     |
| No tool, creation true        | `CreationOperation` |
| Tool supplied, creation true  | Invalid request     |

For example, `mcp:connect` with only a target derives a resource operation, which
the connection definition rejects. Supplying a tool asks about invocation; using
the separate discovery API asks about admission.

The struct API keeps related inputs visible and validates them in one call instead
of making handlers construct operations through separate error-returning builders.
The tradeoff is that Go allows invalid field combinations to be constructed, so the
client must reject them before backend evaluation. The struct alone is not a safety
guarantee, and a request is never itself proof of access.

## Client walkthrough: validation before evaluation

[`authz/client.go`](authz/client.go) is the application-facing boundary. It validates
the question, delegates evaluation, and provides a refusal gate for protected work:

```text
handler supplies subject and request
  → client validates identity, target, permission, and question shape
  → backend loads policy and evaluates the validated question
  → client returns a decision, or Require refuses anything not explicitly allowed
```

`Subject` is defined in `subject.go`; `Decision` carries an outcome, a reason, and
the in-memory policy revision. `Allowed`, `Denied`, and `Unresolved` distinguish a
proven permission from a policy refusal and insufficient facts. A decision or its
revision is diagnostic evidence, not a reusable execution credential.

### Backend contract and startup

The `Backend` interface has concrete authorization, alternatives/groups, filtering,
and discovery operations. `PolicyWriter`, `RoleAssignmentWriter`, and
`CredentialWriter` are separate optional capabilities; write helpers return errors
when the selected backend lacks the corresponding capability. One memory store
implements them all, but an evaluator-only backend need not implement management
writes. These interfaces do not authorize policy-editing workflows. Lookup,
containment, and richer explanation contracts remain unfinished.

`NewClient` performs configuration checks once, including required fields, duplicate
names, referenced scopes in the same resource family, operation forms, and creation
parent support. It copies declarations into an indexed map; this is useful validation,
not a complete generic policy-schema verifier.

`Evaluation` and `Admission` are backend-facing inputs produced by the client. Each
carries the validated subject, target, and permission definition; only `Evaluation`
adds a concrete form and optional tool facts.

### Concrete request processing

`Authorize` processes a request in this order:

1. Validate and copy target data, including mandatory organization ancestry.
2. Validate the explicit principal and authentication kinds, require subject/target
   tenant alignment, and find the declared permission.
3. Derive the operation form from the tool and creation fields.
4. Validate any tool name and disposition, rejecting unsupported values but
   accepting unknown disposition as input to evaluation.
5. Require definition support for the form and check target kind plus required
   ancestors, or the parent kind for creation.
6. Ask the backend to evaluate the resulting question.

The client does not match grants or decide whether unknown disposition matters to
policy; those responsibilities belong to the evaluator. It also cannot establish
truthful resource ancestry or authenticate middleware-supplied identity by shape alone.

### Require versus Authorize and discovery

Most execution handlers use `Require`, which calls `Authorize` and permits work only
for an explicit `Allowed` result. Validation or backend errors are returned directly;
`Denied` and `Unresolved` both produce errors wrapping `ErrDenied` so neither can
start protected work. Applications can inspect a full decision through `Authorize`
when they need to distinguish outcomes.

`DiscoverAccess` separately validates identity, target, permission, required ancestry,
and discovery support before invoking the backend's discovery contract. It returns
a decision rather than the execution gate's error-only result, so admission handlers
must explicitly check for `Allowed`.

### Policy-shape validation is not permission to edit

`ReplaceGrants` validates the receiving principal, known scopes, anchor shape and
eligibility, tenant binding, and supported constraint values before persistence.
The mutation workflow must separately authorize the editor and, where applicable,
prove delegation containment. Supported attributes come from the scope's
`AllowedConstraints`; validating a grant shape does not grant permission to assign it.

The client's role is to keep malformed questions out of evaluation without
becoming a second evaluator or taking over the calling domain's authority decisions.

### Definition-driven grant constraints

`PermissionDefinition.AllowedConstraints` declares which typed attributes may
narrow grants for that scope. The current attribute vocabulary is `ToolConstraint`
and `DispositionConstraint`; this is not an arbitrary string-to-expression map.

For example, the existing MCP definitions now explicitly declare:

```go
AllowedConstraints: []authz.ConstraintKind{
    authz.ToolConstraint,
    authz.DispositionConstraint,
},
```

Every MCP allow and opposing scope declares both attributes, preserving the spike's
existing acceptance behavior. Organization, project, skill, environment, assistant,
and plugin definitions leave the list empty, so constrained grants for those scopes
are rejected. An empty constraints value is still an unrestricted grant within its
anchor, not a wildcard target.

The lifecycle of this declaration is:

1. `NewClient` rejects unknown or duplicate attribute declarations and copies the
   list along with the rest of the permission definition.
2. Grant validation checks each populated field against that scope's allowed
   attributes. Supporting tool names does not automatically support disposition.
3. Existing value validation still rejects wildcard tool names and unknown
   dispositions. Successful shape validation delegates persistence; it does not
   authorize the editor.
4. Evaluation still compares typed constraints against supplied facts. This slice
   changes declarations and validation, not policy-matching or delegation semantics.

The generic client no longer asks whether `definition.Resource == MCPServer` to
decide whether a grant may have attributes. The declaration owns that capability.
Question forms remain separate: allowing a tool constraint on a scope does not
enable tool-operation requests for it. MCP read/write grants can constrain the
tool access they imply, while metadata operations without the necessary facts
remain unresolved when such a constrained grant is needed.

This is not a fully extensible attribute engine: new attribute kinds still need
typed fields, value validation, matching behavior, and any discovery partitioning.
Definition compatibility across implication/exclusion relationships also remains
limited to the existing startup checks, not a full satisfiability proof. No new
`main.go` examples were added; those remain deferred for individual review.

## Admission and policy resolution

Authentication, admission, policy resolution, and evaluation answer different
questions. Authentication establishes the actor and actual represented user.
Admission determines whether that execution context is acceptable. Resolution
loads the actor's and delegator's eligible policy sources. Evaluation decides
whether those policies permit the requested operation.

The memory implementation separates those steps without adding independently
cached services or weakening snapshot consistency:

```text
trusted middleware Subject (authentication is outside this package)
  → ResolvePolicies takes one read lock
    → admitExecutionLocked validates subject and credential binding
    → resolvePolicyLocked loads direct grants, assigned roles, and eligible all-users grants
    → copy each principal's policy with grant provenance and one revision
  → release lock
  → evaluate the complete operation independently within each resolved policy
  → require every policy to allow
```

### Explicit admission, with an intentionally limited trust boundary

[`authz/admission.go`](authz/admission.go) owns the stored-binding checks. Its
private `admittedExecution` is constructed and consumed under the same memory
lock; callers cannot put one in request context or reuse it for another request.
It contains the actor followed by the delegator, if present. Substituting the
delegator or presenting an unknown/mismatched credential refuses before resolution.

This is **not a real authentication system or complete production admission**.
Session subjects remain assertions supplied by trusted fixture code; the package
does not validate a session ID with an identity provider. Credential bindings are
not secret verification, expiry, or revocation. Active membership, agent lifecycle,
owner eligibility, and proof of the actual Slack invoker are still external
obligations. Loading an all-users source must never stand in for membership admission.

### Sources are not execution principals

[`authz/policy.go`](authz/policy.go) defines `GrantSource`, `ResolvedGrant`,
`ResolvedPolicy`, and `PolicyResolution`. A source is a tenant-bound assignment
origin: direct user, direct agent, role, or all users. Role and all-users sources
cannot be passed as executing principals. An all-users source has an empty ID;
the source kind itself identifies its organization-wide user meaning.

For each principal, resolution loads these sources in stable order:

1. Its direct-user or direct-agent source.
2. Its explicitly assigned roles, in stored assignment order.
3. The organization's all-users source, **only for a user**.

An agent inherits only roles assigned to that agent. It never receives the
delegator's roles, owner grants, or `user:all` inside its own policy. A delegator's
sources are resolved independently in a second policy. An absent source has no
grants; resolution synthesizes no role defaults and infers no directory membership.

Every `ResolvedGrant` retains both `Source` and `Grant`, including identical grant
values assigned by different sources. The evaluator receives those sourced values;
it does not flatten away their provenance. Source order is deterministic iteration,
not precedence. The evaluator still applies the same opposing-scope semantics
regardless of source. **The legacy concrete direct-user exception is not implemented
by retaining provenance alone**, and no new mandatory-prohibition model is selected.

### Fixture writes and the resolver boundary

The existing `Client.ReplaceGrants(ctx, principal, grants)` now replaces only that
principal's direct source. It does not erase roles or all-users assignments. Its
call signature remains unchanged so the deferred showcase need not change.

`Client.ReplaceSourceGrants(ctx, source, grants)` writes one direct, role, or
all-users source after validating its shape, supported constraints, and tenant.
`Client.ReplaceRoles(ctx, principal, roles)` replaces explicit role memberships,
rejecting duplicate, non-role, or cross-organization sources before any write.
Empty lists revoke that source's grants or that principal's roles respectively.
Removing a direct grant does not remove independent authority from a role.

These are fixture persistence methods, not directory synchronization, authorized
grant management, or role leases. They do not prove the existence or eligibility
of a principal/role, check editor authority, enforce assignment containment, or
coordinate a multi-source management transaction. A real management domain must
supply those guarantees. User-defined role IDs carry no built-in permissions.

`PolicyResolver.ResolvePolicies` is a backend-side seam. The memory implementation
admits execution and copies memberships, grants, and binding-dependent identity
under one lock. All authorization paths use it, including discovery and empty-batch
filtering. Each receives a new resolution rather than a grant bag cached in request
context. The exported result is detached data, not a token accepted by the client;
mutating an inspection result cannot modify stored policy. Production endpoints
must not expose these raw grants without a separate read-authorization contract.

The resolver is not a second independently selected provider in `NewClient`:
backend implementations own its consistency with evaluation. A database backend
would need an equivalent transaction/revision contract rather than separately
loading actor and delegator from arbitrary snapshots. Choosing how to reuse a
snapshot within a real request, and how to refresh it for long-lived work, remains
a production design question. No context helper or indefinite cache is added here.

**Slice boundary:** `main.go` is untouched. The existing showcase still uses only
direct sources; it does not demonstrate the new role/all-users paths. No production
authentication, directory sync, direct-user exception, credential ceiling, or role
redesign is implemented by this slice. Examples remain deferred for individual review.

Validation included the unchanged showcase, package vetting, and a temporary
out-of-tree smoke program checking provenance, detached snapshots, live role
revocation, user-only all-users grants, independent agent/delegator authority,
substituted-delegator refusal, invalid membership rejection, cancellation, and an
evaluator-only backend refusing writes. No test files were added; these checks
are not evidence of production authentication or distributed consistency.

## Evaluator walkthrough: interpreting one policy

[`authz/evaluate.go`](authz/evaluate.go) interprets one grant collection. It does
not authenticate callers, load stored policy, or combine independent permission
limits. The backend passes grants, a validated `Evaluation`, and the policy revision:

```go
func evaluate(grants []ResolvedGrant, question Evaluation, revision uint64) Decision
```

The question is whether the subject's assigned policy permits the operation.
There is no owner-policy or credential-policy intersection. Domain lifecycle,
risk, and upstream checks remain separate from this grant evaluation.

### Scope, boundary, and constraint matching

Each grant passes through three filters:

1. **Relevant scope:** the requested permission itself, a scope directly listed in
   `ImpliedBy`, or an opposing scope listed in `ExcludedBy`. Unrelated scopes are
   ignored, with no automatic recursive implication traversal.
2. **Matching anchor:** the anchor kind must be permitted, and the full anchor
   reference must equal the target resource or one of its ancestors. A Project A
   grant can cover its server, while a Project B grant cannot.
3. **Matching constraints:** compare policy tool/classification restrictions with
   the invocation facts, keeping insufficient facts distinct from a definite mismatch.

`constraintMatch` uses the outcome values for a local matching result, not a complete
authorization decision:

| Constraint comparison                                     | Match result |
| --------------------------------------------------------- | ------------ |
| No constraints                                            | `Allowed`    |
| Required tool name differs                                | `Denied`     |
| Required disposition differs from a known classification  | `Denied`     |
| Required disposition is unknown                           | `Unresolved` |
| Constraints require facts but no tool facts were supplied | `Unresolved` |
| All populated constraints match                           | `Allowed`    |

An unconstrained grant can match an unclassified invocation because it does not
require a classification. Conversely, unknown classification cannot establish a
read-only constraint or safely dismiss a potentially applicable classification block.

### Allows and exclusions are tracked separately

The evaluator tracks `allowed`, `uncertainAllow`, and `uncertainBlock`. These record
a definite matching allow, an unresolved potentially allowing grant, and an unresolved
potentially opposing grant respectively. A definite matching opposing grant returns
denial immediately; finding an allow does not return immediately because a later
exclusion could still remove it.

The resulting decision follows these rules:

| Evidence                                                              | Outcome      |
| --------------------------------------------------------------------- | ------------ |
| A definitely matching opposing grant                                  | `Denied`     |
| No definite or possible allow                                         | `Denied`     |
| Only an uncertain allow                                               | `Unresolved` |
| A definite allow with an uncertain applicable exclusion               | `Unresolved` |
| A definite allow without a definite or uncertain applicable exclusion | `Allowed`    |

An uncertain additional allow does not defeat a definite allow. An uncertain
exclusion prevents a proven allow because the operation may still be excluded.

For example, with an allow for all Project A tools and a named block for
`delete_document` on Server A, `list_documents` matches the allow but not the block,
so the policy permits it. `delete_document` matches both and is denied. The concrete
handler flows below demonstrate those boundaries through the client.

The evaluator is pure policy interpretation: the revision is passed in for the
decision record, not loaded here. Principal-specific exception precedence, restricted
audiences, and whole-policy delegation proofs remain unfinished; this subtraction
algorithm must not be presented as complete production compatibility.

## Two concrete handler flows

The runnable showcase includes both flows below, with extra calls demonstrating
classification mismatch and missing facts. Their fixtures are setup policy, not
examples of authorized policy-editing endpoints.

### Project read-only grant permits an invocation

`projectReadOnlyFlowExample` uses this policy for `project-reader`:

```go
authz.Grant{
    Permission: authz.MCPConnect,
    Anchor: authz.ResourceRef{
        OrganizationID: "example-org",
        Kind:           authz.Project,
        ID:             "project-a",
    },
    Constraints: authz.Constraints{Disposition: authz.ReadOnly},
}
```

The request invokes `list_documents` on Server A in Project A:

```go
err := client.Require(ctx, subject, authz.Request{
    Permission: authz.MCPConnect,
    Target:     server.AuthzTarget(),
    Tool: &authz.ToolFacts{
        Name:        "list_documents",
        Disposition: authz.ReadOnly,
    },
})
```

The authorization flow is:

1. The handler loads the server within the authenticated organization and obtains
   the tool's classification from stored metadata.
2. The client infers the tool form, checks its support in the definition, and
   validates tenant binding and required ancestry.
3. The backend loads current policy, then matches the project anchor against the
   server's trusted ancestry and the read-only constraint against the tool facts.
4. With a matching allow and no matching exclusion, `Require` returns nil
   and the handler reaches simulated protected execution.

In the same example, `delete_document` is denied because its known destructive
classification does not match, while unclassified `lookup` is unresolved and also
refused. In the latter case the grant's reach matches, but its constraint is unproven.

### Connection succeeds but a named tool is blocked

`blockedDeleteFlowExample` uses two grants for `delete-blocked-user`:

```go
grants := []authz.Grant{
    {Permission: authz.MCPConnect, Anchor: serverRef},
    {
        Permission: authz.MCPBlockedConnect,
        Anchor:     serverRef,
        Constraints: authz.Constraints{Tool: "delete_document"},
    },
}
```

The connection handler asks whether any usable policy remains:

```go
decision, err := client.DiscoverAccess(
    ctx, subject, authz.MCPConnect, server.AuthzTarget(),
)
```

Discovery returns `Allowed` because blocking one named tool does not remove all
tool-use policy. Later the ordinary invocation handler checks `delete_document`:

```go
err := client.Require(ctx, subject, authz.Request{
    Permission: authz.MCPConnect,
    Target:     server.AuthzTarget(),
    Tool: &authz.ToolFacts{
        Name:        "delete_document",
        Disposition: authz.Destructive,
    },
})
```

Both grants cover the server, but the opposing grant also matches this tool name,
so evaluation subtracts that invocation and `Require` refuses it. A subsequent
`list_documents` call succeeds because its name does not match the opposing grant.
Connection admission never substitutes for the later concrete authorization check.

## Superseded decision: grant management instead of runtime ceilings

**Correction after the agent/delegator discussion:** the decision below assumed
that the runtime ceiling constrained an agent by its owner's policy. That is not
the intended guarantee. An agent can be invoked by someone other than its owner;
owner grant reconciliation cannot stop that invoker from borrowing the agent's
greater authority. Conversely, delegator-only evaluation could let an agent exceed
its assigned purpose when used by a powerful delegator.

The agreed semantics therefore require both agent authority and the current
delegator's authority to cover the same operation. Ownership and delegation must
remain distinct. The spike now models one trusted agent/user delegation and applies
that bound to concrete checks, alternatives, discovery, and filtering.
Scheduling and delegation chains still
need explicit context rules. No owner-based ceiling should be restored by inference.
The historical rationale below is preserved as a record of the rejected assumption,
not a statement of current design approval.

We rejected the earlier design that intersected agent, live owner, and credential
policies on every operation. It made an agent's authority depend on additional
principals at execution time and mixed grant management with authorization.
`ceilings.go`, owner-policy loading, credential grant collections, and their
intersection examples have been removed from the spike. Earlier successful runs
demonstrated that discarded behavior, not evidence for the replacement lifecycle.

An agent is an independently granted principal. Execution evaluates its assigned
grants; a credential is bound to that principal and has no separate permission set.
Narrower automation access is expressed through the agent's grants, not another
runtime policy. Ownership remains a management/lifecycle relationship outside the
generic authorization evaluator.

The management rules proposed at that historical point were:

- An administrator may give users permission to create agents.
- A creator cannot assign an agent permissions beyond their own delegable access.
  Grant expansion also needs editor authorization and assignment containment.
- Permission increases for an owner do not automatically expand agent grants.
- Permission reductions disable that owner's agents pending explicit review;
  re-enabling requires authorized review of their grants against current authority.
- Removing an owner from the organization disables their agents and prevents
  continued execution through already-issued credentials.

Disable-on-reduction is deliberately conservative: it disables all of that owner's
agents rather than calculating which individual grants were affected. This can stop
unrelated automation, but avoids live permission intersections and selective grant
rewriting. A production implementation must cover indirect reductions through roles
and resource access, make disablement effective alongside the reduction, and check
agent-enabled state before execution. Async cleanup alone would leave a retention
window. In-flight work and ownership transfer need explicit lifecycle semantics.

**Historical boundary at removal:** the spike evaluated one assigned grant collection
and validates credential binding. It does not yet implement creation permission,
assignment containment, owner storage, enabled state, disable-on-reduction/removal,
or re-enablement. `ReplaceGrants` and `BindCredential` are validated fixture
persistence, not secure management endpoints. Removing ceilings does not implement
those replacement safeguards; this remains unsuitable for production integration.

The unified RFC still describes the earlier ceiling model in places. This decision
is recorded here for the isolated spike; reconciling the production proposal is
separate work, not evidence that production semantics have changed.

## Credential walkthrough: identity binding, not another policy

[`authz/credential.go`](authz/credential.go) provides `BindCredential(ctx, principal,
credentialID)`. It validates the typed, tenant-bound principal and requires a
concrete credential ID before delegating persistence. It receives no grants and
does not authorize issuance; a management handler must authorize that separately.

The memory backend stores `(organization, credential ID) → execution binding` and
refuses rebinding to another principal or delegator. `BindCredential` supports
direct users; `BindDelegatedCredential` records an agent plus represented user.
At execution the binding must match the authenticated subject, including delegation.
The backend then evaluates either the direct user's grants or both the agent and
delegator policies.
User and agent identities with the same textual ID remain distinct. Binding does
not verify a token or session, implement expiry/revocation, or check agent-enabled
state; authentication and lifecycle enforcement still need their own boundaries.

## Main walkthrough: following one request end to end

[`main.go`](main.go) assembles the components without putting backend details into
handlers. `run` creates the memory backend and client with the MCP, organization,
project, skill, environment, assistant, and plugin definitions, seeds
synthetic grants and credential bindings, and injects the client into `service`.
These setup writes are fixtures, not examples of authorized agent provisioning.

For a `list_documents` invocation, the path is:

```text
authenticated subject + server ID + tool name
  → invokeToolHandler
  → organization-scoped server lookup
  → stored tool classification + server.AuthzTarget()
  → client.Require(Request{Permission, Target, Tool})
  → client validates the question
  → backend admits the asserted execution and resolves sourced policies coherently
  → evaluate matches scope, anchor, and constraints
  → Require permits only Allowed
  → simulated protected execution
```

`serverRow.AuthzTarget` owns resource adaptation: it supplies the server identity
and its project and organization ancestors from trusted stored data. The caller
does not submit those relationships. Similarly, the caller names the tool but the
handler reads its classification from stored metadata. Neither an organization-scoped
lookup nor valid target structure grants permission by itself.

The handler's authorization request remains concise:

```go
if err := s.authz.Require(ctx, subject, authz.Request{
    Permission: authz.MCPConnect,
    Target:     server.AuthzTarget(),
    Tool:       &authz.ToolFacts{Name: toolName, Disposition: disposition},
}); err != nil {
    return "", err
}
```

The domain chooses `MCPConnect` for invocation. A different handler chooses
`MCPRead` for server inspection; the generic client does not infer business
permissions from handler names. `connectHandler` instead asks `DiscoverAccess`
whether any usable tool policy remains and explicitly checks for `Allowed`.
Connection admission never replaces the later concrete invocation check.

An agent follows the same handler path as a user. Its explicit principal kind
selects a distinct policy identity; delegated execution adds the current delegator's
policy, never an owner-policy lookup. The `exampleUserSession` and `exampleAgentCredential`
helpers only construct synthetic middleware output; they are not authentication.

The final string returned by `invokeToolHandler` stands in for execution. Real
execution still needs lifecycle admission, including agent-enabled checks, plus
applicable risk, entitlement, and upstream credential checks. Those safeguards are
not demonstrated merely because the grant check succeeds.

## Organization and project scope mechanics

This slice extends the same evaluator with declarations in
[`authz/organization.go`](authz/organization.go) and
[`authz/project.go`](authz/project.go). Startup combines those ordered declarations
with the MCP definitions; no organization/project-specific evaluator was added.

The operation gates are:

| Operation                  | Required scope  | Target                                            |
| -------------------------- | --------------- | ------------------------------------------------- |
| Read organization metadata | `org:read`      | Organization itself, with no ancestors            |
| Create a project           | `org:admin`     | Existing organization, not an invented project ID |
| Read an existing project   | `project:read`  | Project with its organization ancestor            |
| Edit an existing project   | `project:write` | Project with its organization ancestor            |

Project creation follows the current domain gate in
`server/internal/projects/impl.go`: there is no `project:create` scope. The handler
checks organization administration as a normal resource operation before creation.
It does not set `Creation: true`, because the requested permission describes the
organization operation, not a project-family creation permission. The generic
creation form remains available for definitions that explicitly declare it.

`org:admin` implies `org:read`; `project:write` implies `project:read`. Neither
organization administration nor project access implies MCP use, skill access, or
other unrelated scope families. An administrator role can assign those scopes
explicitly; the `org:admin` scope is not a global bypass or a role-membership flag.
An organization-anchored `project:write` grant covers existing projects in that
organization, but still does not authorize creating projects.

Opposing scopes are directional. A write/admin block removes that stronger
operation without removing read. A read block also removes write/admin, matching
the current non-MCP exclusion expansion. Because the spike does not recursively
expand implications, both applicable opposing scopes are listed explicitly in
the stronger permission's definition. MCP's independent opposing scopes remain
unchanged. Principal-specific exception precedence is still not implemented.

`organizationProjectExample` exercises successful creation admission, admin-to-read
and write-to-read implications, denied cross-family operations, and directional
blocks. Creation and update handlers only return simulated success strings; they
do not persist projects or implement provisioning. Grant management, agent
containment, and owner lifecycle work remain outside this walkthrough's scope and
are handled separately.

Next slices cover the remaining scope families rather than introducing new scope
names merely for example actions.

## Skill scope mechanics: existing resources and creation parents

[`authz/skill.go`](authz/skill.go) declares `skill:read`, `skill:write`, and their
opposing scopes. Write implies read; a read block opposes both operations, while a
write block leaves read intact. Grants may be anchored at one skill, its project,
or its organization. Existing skill targets require project and organization
ancestry, supplied by the trusted `skillRow.AuthzTarget` adapter in `main.go`.

Reading or editing a skill uses a normal resource request. Creating a skill instead
uses the existing project as the target, with the creation flag:

```go
if err := s.authz.Require(ctx, subject, authz.Request{
    Permission: authz.SkillWrite,
    Target:     project.AuthzTarget(),
    Creation:   true,
}); err != nil {
    return "", err
}
```

The definition declares `CreationParent: Project`. The client therefore validates
a project parent instead of requiring an already-existing skill, and the evaluator
matches grants against that parent and its organization ancestor. Project-wide or
organization-wide skill write can authorize creation; exact-existing-skill write
cannot authorize arbitrary siblings. An exact skill block likewise does not prevent
creating another skill, whereas a project skill block can prevent creation there.
No future resource ID or wildcard target is fabricated.

The runnable `skillExample` demonstrates these boundaries:

- Project skill write allows creation, reading through implication, and updates.
- A project grant does not cover skills or creation in another project.
- Exact skill write allows updating that skill but not creating a sibling.
- Organization skill read covers skills across projects but cannot authorize creation.
- Write blocks retain read; read blocks also remove write.
- Skill permissions do not grant project metadata access or MCP tool use, and
  project write does not grant skill access.

The evaluator and backend have no new skill-specific branches. Skill requests have
no tool facts; tool/disposition constraints remain invalid for non-MCP grants.
Skills are not discoverable through the MCP connection-admission API. Distribution
and publication require their own domain gates, not merely skill read/write.

**Production qualification:** current `server/internal/skills/impl.go` creation
uses project-wide `skill:write`, and some reads support exact-skill access through
`requireSkillAccess`. Update and several other endpoints still use project-wide
`requireAccess`. This spike demonstrates a uniform resource-level edit model; it
does not assert that exact-skill edit grants work on every current endpoint.
Feature gating, collection filtering, restricted audiences, distribution, and
publication behavior are not implemented by these examples. Creation and updates
only return simulated authorization success; no resources are persisted.

## Environment scope mechanics: secret access is independent

[`authz/environment.go`](authz/environment.go) declares environment read/write and
opposing scopes using the same resource and creation forms as skills. Existing
environment targets carry trusted project and organization ancestors. Grants can
cover one environment, its project, or its organization; write implies read.

Environment read can expose secret values, so project read/write, organization
administration, skill access, and MCP access do not imply it. Built-in roles may
assign environment scopes explicitly. The resolver loads explicit role assignments
but does not provision or silently restore defaults. The examples store only environment identity and ancestry,
not secret values.

Creation asks for environment write covering the existing project parent:

```go
if err := s.authz.Require(ctx, subject, authz.Request{
    Permission: authz.EnvironmentWrite,
    Target:     project.AuthzTarget(),
    Creation:   true,
}); err != nil {
    return "", err
}
```

An exact-environment grant cannot authorize sibling creation. Project grants cannot
cover another project's environments, while organization grants can cover both.
Write blocks retain read; read blocks remove both read and write; a project block
can prevent creation under that project. Tool facts and MCP discovery are not
supported for environment operations.

`environmentExample` exercises those cases and explicitly refuses environment reads
for project readers/editors, organization-admin-only callers, and skill editors
without environment grants. The creation and update handlers simulate authorization
only; they persist no environment and load no secret values. The evaluator remains
unchanged.

**Production qualification:** `server/internal/environments/impl.go` already keeps
environment scopes separate from project scopes and checks project-scoped environment
write for creation. Its current creation check represents the project boundary using
an environment-kind selector and a project dimension. This spike replaces that
encoding with an explicit project-parent creation question; no production code has
changed. Source/toolset linking can expose secrets and requires separate domain
gates, including environment read. Link operations, clone source/destination checks,
collection filtering, slug-miss response behavior, encryption, and default environment
provisioning are not implemented or claimed as parity by this slice.

## Assistant scope mechanics: configuration is not execution authority

[`authz/assistant.go`](authz/assistant.go) declares assistant read/write and their
opposing scopes. Existing assistants carry project and organization ancestry;
grants can cover one assistant, a project, or the organization. Write implies read.
A write block retains read; a read block removes both. The generic evaluator and
backend are unchanged.

Reading configuration and editing an existing assistant are resource operations.
Creation requires assistant write covering the existing project parent:

```go
if err := s.authz.Require(ctx, subject, authz.Request{
    Permission: authz.AssistantWrite,
    Target:     project.AuthzTarget(),
    Creation:   true,
}); err != nil {
    return "", err
}
```

An exact-assistant grant cannot authorize arbitrary sibling creation. Project-wide
grants do not cover another project; organization-wide grants can. A project-wide
assistant block prevents creation there. Tool facts and MCP discovery are not
supported question shapes for this family.

`assistantExample` demonstrates those cases plus denied access for project editors,
organization-admin-only callers, and skill editors without assistant grants.
Assistant write also fails to authorize MCP invocation: configuration access does
not become authority to execute the configured tools. These fixtures assign scopes
explicitly and do not implement system-role defaults.

**Production qualification:** current `server/internal/assistants/impl.go` uses
assistant read/write for individual resources and project-scoped assistant write
for creation. The spike models the latter as an explicit parent-creation question
instead of substituting a project ID into an assistant selector. Production also
uses assistant read as one gate for sending messages, but that path requires user
identity, continued-chat ownership, and additional gates for supplied skills.
This slice only demonstrates configuration read, update, and creation; it does not
implement messaging or imply transcript access. Attachment authorization, runtime
workload identity, agent grant management, triggers, collection filtering, and
upstream/tool authorization remain separate work. Creation and update are simulated
authorization results, not persisted mutations or runtime provisioning.

## Plugin scope mechanics: content is not member authority

[`authz/plugin.go`](authz/plugin.go) declares `plugin:write` and
`plugin:blocked_write`. There is no `plugin:read` scope in the current vocabulary,
so none is invented. Explicit content-write grants may cover a plugin, its project,
or its organization; an existing plugin requires project and organization ancestry.
Creation uses `PluginWrite` with `Creation: true` and the trusted project parent.
An exact-plugin grant cannot authorize sibling creation, and a project block can
prevent both content edits and creation there.

`pluginExample` demonstrates exact/project/organization grants, wrong-project
denials, and opposing scopes. The handlers now compose plugin write and organization
admin through exclusion-aware OR. The examples also show that
plugin write does not authorize MCP invocation, edits to a referenced skill, or
generic project administration. These checks demonstrate separation of scopes,
not actual bundle membership: fixtures contain plugin identity and ancestry only.
Creation and content updates are simulated; no package or audience is changed.

**Production qualification:** current
`server/internal/authz/plugin_write.go` permits either project-scoped plugin write
or organization admin, with explicit exclusions defeating either alternative.
The spike now implements that organization-admin alternative. Exact-plugin
edit grants are an illustrative resource-level extension; current mutations gate
on project-scoped content access. Organization-anchored plugin write in the spike
is distinct from holding the `org:admin` scope.

Current inventory reads in `server/internal/plugins/impl.go` allow organization
read first, otherwise plugin-write authorization. A plugin-write block must not
remove independent organization-read inventory access. Recipient identities and
counts have a separate organization-admin gate. These rules belong to domain
composition, not cross-resource permission implication. The grouped composition
below now demonstrates the read gate on one plugin's metadata; audience management
and complete paginated inventory responses remain unfinished. The full read rule
includes an exclusion-aware write fallback and cannot be replaced by flattening
every branch into an ordinary OR.

Publication, installed-client refresh, source/member authorization, distribution,
default-plugin provisioning, and access approval side effects remain separate
domain workflows. Plugin content access never substitutes for authorization to
execute the bundled server's tools or read a skill's content.

## Alternatives walkthrough: OR without losing exclusions

[`authz/alternatives.go`](authz/alternatives.go) adds two execution gates for the
same subject. The domain supplies the alternatives and chooses their composition:

- `RequireAny`: ordinary OR. Any allowed branch is sufficient; another branch's
  exclusion does not defeat independent access.
- `RequireAnyUnblocked`: an allow is required, and applicable opposing scopes on
  any branch defeat the combined operation. An unresolved exclusion prevents a
  proven allow even when the excluded branch has no allowing grant of its own.

For plugin content editing the handler asks:

```go
if err := s.authz.RequireAnyUnblocked(ctx, subject,
    authz.Request{Permission: authz.PluginWrite, Target: plugin.AuthzTarget()},
    authz.Request{Permission: authz.OrgAdmin,
        Target: organizationTarget(subject.Principal.OrganizationID)},
); err != nil {
    return "", err
}
```

The creation handler supplies a project-parent creation request for the first
branch and the same organization-admin resource request for the second. This is
domain composition across resource families, not an `ImpliedBy` relationship or a
global admin bypass.

`Decision.Exclusion` carries structured `NoExclusion`, `DefiniteExclusion`, or
`UncertainExclusion` evidence on concrete checks. It is separate from the outcome:
a branch can be denied because it has no allow while still carrying an unresolved
exclusion that matters to a different branch's allow. Discovery decisions leave
this field unset; they are not concrete alternatives in this API.

The combination rules are:

| Evidence                                                        | Ordinary OR               | Exclusion-aware OR |
| --------------------------------------------------------------- | ------------------------- | ------------------ |
| At least one allow, no exclusions                               | Allowed                   | Allowed            |
| At least one allow, definite exclusion on another branch        | Allowed                   | Denied             |
| At least one allow, uncertain exclusion on another branch       | Allowed                   | Unresolved         |
| No allow, at least one unresolved branch, no definite exclusion | Unresolved                | Unresolved         |
| No allow or unresolved branch                                   | Denied                    | Denied             |
| Any definite exclusion, even alongside an unresolved branch     | Depends on other branches | Denied             |

The client validates every request before calling the backend; malformed
alternatives are errors, not ignorable denials. Empty alternatives are rejected.
The memory backend checks the mode and same-subject invariant, loads one coherent
grant/binding snapshot, and evaluates all branches at that revision. It does not
short-circuit on an allow before checking the other exclusions. Binding/backend
errors propagate rather than turning into another branch's success. No decision
logic parses human-readable reason strings.

`alternativesExample` demonstrates admin fallback, blocks defeating either write
alternative, ordinary OR preserving independent org-read access, an uncertain
classification block with no allowing tool grant, missing permissions, empty OR,
and malformed alternatives. These are runnable demonstrations, not tests or a
proof of production parity. Two-level grouping is implemented below; arbitrary
recursive expressions, discovery composition, complete inventory responses, and
richer public decision-returning composition APIs remain unfinished. The single-level
delegator bound now applies to these checks as described below.

## Grouped alternatives: plugin metadata read paths

[`authz/groups.go`](authz/groups.go) adds a deliberately bounded two-level form:
ordinary OR across groups, each containing ordinary or exclusion-aware alternatives.
There is no recursive expression language, and a handler does not sequence separate
grant reads. `RequireAnyGroups` validates every group and request, then the backend
evaluates every leaf using one subject's grant/binding snapshot and policy revision.
The top level and every group must be nonempty, modes must be known, and the total
number of leaf requests is limited to `MaxGroupRequests` (100).

`readPluginMetadataHandler` implements this domain rule:

```text
org:read on the organization
  OR independently
    (plugin:write on the plugin OR org:admin on the organization,
     with exclusions shared within this fallback)
```

The handler declares two `AlternativeGroup` values: one organization-read request,
and one `UnblockedAlternatives` group containing plugin write and organization
admin. Exclusions on the second path do not defeat a proven allow from the first.
The existing alternative combiner is reused for each group and for the outer OR;
no reason strings or special plugin grant matching are introduced.

`pluginReadExample` demonstrates the important differences:

- Plugin writers can read metadata through the fallback without organization read.
- Organization readers retain metadata access despite a plugin-write block.
- A plugin writer with no org-read access and an explicit org-admin block is refused:
  the block defeats the fallback. A flat ordinary OR would incorrectly admit them.
- Callers with no applicable allow, or a blocked write path without independent
  read access, are refused.
- An empty nested group fails validation even when a different group would allow.

This is a **single-plugin metadata read gate**, not a full inventory endpoint.
The spike permits exact-plugin writes as a resource-level extension; current
production's fallback uses project-wide content authorization. Recipient identities
and counts are intentionally absent, and this check neither reveals bundled skill
content nor authorizes MCP execution. Listing, pagination, audience administration,
and production response filtering remain separate. The group API does not combine
different subjects. The backend applies the agent/delegator bound around the
complete grouped decision, not by merging their grants.

### RequireAll, RequireAny, groups, and filtering

These APIs answer different questions:

| API / concept                                | Meaning                                                           | Use                                                            |
| -------------------------------------------- | ----------------------------------------------------------------- | -------------------------------------------------------------- |
| `Require`                                    | One concrete question must be allowed                             | Read or edit one resource                                      |
| `RequireAll` (concept only; not implemented) | Every question must pass, A AND B                                 | An operation requiring two separate authorities                |
| `RequireAny`                                 | Any branch may allow, with exclusions local to each branch        | Independent ways to access something                           |
| `RequireAnyUnblocked`                        | Any branch may allow, but applicable exclusions on any branch win | Plugin content write or organization-admin fallback            |
| `RequireAnyGroups`                           | Ordinary OR across groups, each with its own exclusion semantics  | Organization read OR the exclusion-aware plugin-write fallback |
| `Filter`                                     | Return the allowed subset of resource candidates                  | Build a list response without exposing denied rows             |

Groups provide the parentheses that flat APIs lack. Flattening the plugin-read
rule into `RequireAny` would admit a plugin writer whose admin fallback is explicitly
blocked and who has no independent organization read. Flattening it into
`RequireAnyUnblocked` would instead deny an organization reader because plugin write
is blocked. AND would incorrectly demand both permissions. The grouping structure
expresses the existing rule; its API name is a spike choice, not a commitment to a
general-purpose expression language.

Filtering is not another AND/OR gate. It returns zero or more allowed candidates,
while `Require*` either permits or refuses one protected operation. Each candidate
uses the same decision rules and, for delegated agents, must be allowed by both
agent and delegator. Zero visible candidates is a valid list response; it must not
expose denied candidate metadata or become permission for subsequent mutations.

## Collection walkthrough: filter candidates, not individual checks

[`authz/filter.go`](authz/filter.go) adds `Client.Filter` for bounded existing-resource
collections. The domain loads trusted candidates within the authenticated tenant,
adapts them to targets, and supplies the required scope:

```go
allowed, err := s.authz.Filter(ctx, subject, authz.ProjectRead, targets)
```

The result contains only allowed resource references, in candidate order. The
handler builds its response from those references, not from all loaded rows. An
empty result is a successful list with no visible resources, not permission to
expose candidate names, counts, or metadata. List authorization does not replace
later authorization of a concrete mutation.

The client validates the subject, permission, resource-operation support, and
every target before the backend runs. Duplicate resource references, malformed or
cross-tenant candidates, unknown scopes, and tool-only scopes are errors. The
spike bounds batches to `MaxFilterCandidates` (100); it does not silently truncate.
This bound limits authorization work, not the repository scan or response bytes.

The memory backend validates credential binding and reads the principal's grants
once, including for an empty batch. It evaluates every candidate at the same policy
revision, returning only explicit allows. Missing permissions, opposing grants,
and unresolved matches omit a resource. Backend/binding errors and observed context
cancellation fail the batch without returning partial results. There is no loop
of separate `Require` calls and no owner-policy evaluation.

`listProjectsHandler` simulates one bounded repository page, using a tenant-filtered,
sorted candidate list. `collectionExample` demonstrates:

- A single-project editor sees only that project through write-to-read implication.
- A write block retains read visibility; a read block removes the project.
- An organization-anchored project-read grant covers both projects, while
  organization admin alone does not imply project visibility.
- Removing the list reader's grants produces an empty list on the next request.
- A malformed later candidate fails the whole batch, an invalid credential fails
  even an empty batch, and MCP tool-only connect cannot filter resource reads.

This is **candidate filtering, not policy-driven lookup or production pagination**.
A database may need to scan more candidates to fill a visible page, and an empty
filtered candidate page does not prove there are no accessible resources later.
Authorized totals, stable pagination under mutation, list/query budgets, repository
pushdown, ordering across pages, nested alternative filtering, and a production
freshness contract remain unfinished. No list totals or continuation tokens are
invented by the showcase, and no real project data is exposed.

## Delegation walkthrough: agent and current delegator, not owner

[`authz/delegation.go`](authz/delegation.go) adds the runtime ceiling needed when
someone invokes an agent with authority different from their own:

```text
effective operation = assigned agent authority ∩ current delegator authority
```

An agent with write cannot lend that authority to a read-only delegator. A broadly
privileged delegator likewise cannot expand a read-only agent. The owner is not
consulted, and no owner relation or credential grant collection has been restored.

`Subject.Delegation` explicitly selects direct or delegated execution. Direct user
sessions and credentials evaluate the user's policy. Agents require delegated
credential execution with one same-organization user delegator; an absent context,
direct-agent mode, cross-tenant delegation, and agent-as-delegator chains are refused.
This intentionally leaves independent scheduled agents unsupported rather than
silently assuming an owner, bypassing the ceiling, or inventing a delegator.

`BindDelegatedCredential(ctx, agent, delegator, credentialID)` records a fixture
binding to the exact agent and represented user. The backend checks that binding
against the subject and refuses substituting a more powerful delegator. These are
synthetic authenticated-context fixtures, **not a production credential design**:
real middleware/issuance must authenticate who is represented and authorize use
of the agent. Merely holding an agent's reusable credential must not let a caller
claim a different user. Expiry, credential exchange, secure issuance, membership,
agent-enabled checks, and invocation eligibility are not implemented here.

The memory backend snapshots the binding and both grant collections under one
lock. A policy intersection requires explicit allows from both collections; denial
wins over uncertainty, and uncertainty still refuses execution. The same boundary
is applied consistently:

- Concrete requests evaluate the same request in both policies.
- OR and grouped checks evaluate the complete domain expression independently
  within each policy, then intersect the decisions. The policies may each allow
  through a different legitimate domain alternative; their grants are never merged.
- Filtering keeps a resource only when both policies allow that candidate.
- Discovery searches for the same tool/disposition region surviving both policies.
  A list-only agent and lookup-only delegator cannot connect merely because each
  independently has some access.

### Identity, authentication, and delegation answer different questions

The subject carries three independent pieces of trusted context:

| Field            | Question                                | Delegated execution                                         |
| ---------------- | --------------------------------------- | ----------------------------------------------------------- |
| `Principal`      | Who is executing?                       | The agent whose assigned grants constrain its purpose       |
| `Authentication` | How was this execution authenticated?   | A credential identifier checked against a stored binding    |
| `Delegation`     | On whose behalf is the agent executing? | The current represented user's typed, tenant-bound identity |

The delegator is not automatically the creator, owner, person who installed an
integration, or last person to edit the agent. The trusted invocation/authentication
boundary must establish the represented user. Ownership cannot substitute for
that proof, and caller-supplied request fields cannot select a more powerful user.

The context is structurally represented as:

```go
subject := authz.Subject{
    Principal: agent,
    Authentication: authz.Authentication{
        Kind: authz.CredentialAuthentication,
        ID:   credentialID,
    },
    Delegation: authz.Delegation{
        Kind:      authz.DelegatedExecution,
        Delegator: invokingUser,
    },
}
```

This is a data shape, not authentication code. `agent` and `invokingUser` are
validated `Principal` values. A separate, domain-authorized issuance step must
have bound this credential to precisely that execution context. The spike's binding
is deliberately immutable: a different represented user requires a different
credential binding, not editing the subject or rebinding an existing credential.
That fixture mechanism does not establish how production should exchange or
mint credentials for each invocation.

### The path through the implementation

The execution path keeps validation, stored trust, and policy interpretation separate:

1. `validateSubject` calls `validateDelegation` before policy evaluation. Direct
   mode requires a user with no delegator. Delegated mode requires an agent and a
   valid user delegator in the same organization. Authentication validation also
   refuses agent human-session combinations.
2. `admitExecutionLocked`, called by `ResolvePolicies`, checks the credential binding against both the agent
   and delegation context. Unknown credentials, a different actor, or a substituted
   delegator fail before any permission can allow execution.
3. Under one read lock, `resolvePolicyLocked` copies each principal's direct,
   assigned-role, and eligible all-users grants into separate `ResolvedPolicy`
   values inside `PolicyResolution`, retaining each grant's source. The delegator's
   permissions are loaded live, not embedded in the credential. There is no ownership lookup.
4. `PolicyResolution.intersect` calls the supplied decision function once for each
   policy. For a simple request this function is `evaluate`; for OR/groups it
   evaluates the complete composed operation. This callback keeps the bound reusable
   without duplicating agent/delegator rules inside every operation evaluator.
5. Both policies must return `Allowed`. A definite denial wins over an unresolved
   answer; absent a denial, any unresolved answer prevents execution. An empty
   snapshot is denied defensively. Failure reasons identify `principal policy` or
   `delegator policy`, rather than exposing the identities or raw grants.

The following relationship is an intersection, not an alternative:

```text
agent allows operation AND delegator allows operation
```

Within either policy the operation may itself have legitimate OR alternatives.
Those alternatives are evaluated separately inside each policy before the AND.
This is why grants must not be merged: a permission present only on the agent
cannot compensate for a missing delegator permission, or vice versa.

### Live changes and the limits of the guarantee

If either policy is narrowed, the next authorization snapshot observes the new
grants without rewriting the other principal or revoking every credential. If the
delegator gains authority, only operations already permitted by the agent can
become effective; agent authority remains the other bound. Removing either side's
last relevant grant prevents the operation. This describes subsequent checks, not
cancellation of already-running work or a transaction spanning authorization and
execution.

The ceiling answers whether both parties have authority, not whether this person
may invoke this particular agent at all. Invocation eligibility, credential
possession/authentication, membership, and lifecycle admission remain separate
requirements. In particular, a shared credential prebound to a privileged user
does not prove that a less-privileged person invoking through Slack is that user.
Correctly identifying the current delegator is essential to the bound's guarantee.

### Existing demonstration coverage

`delegationExample` demonstrates a broad agent constrained by a list-only delegator,
common project visibility, refused plugin alternatives/groups, disjoint discovery,
credential-context substitution refusal, direct-agent bypass refusal, and live
delegator revocation with an already-issued credential. Earlier limited-agent
examples use a broad delegator and demonstrate the opposite bound. Handler request
syntax stays unchanged; trusted subject construction supplies the delegation.

Grant management and lifecycle workflows remain separate work. This slice models
one live delegation edge only; it does not claim production parity for Slack users,
background scheduling, delegation chains, shared invocations, or distributed policy
freshness. The unified RFC still needs reconciliation with these APIs and the
corrected meaning of delegator versus owner.

Further `main.go` examples are deferred: subsequent slices should focus on the
mechanics and their documentation, with examples reviewed one at a time at the end.

## Chat scope mechanics: explicit content grants, not metadata visibility

[`authz/chat.go`](authz/chat.go) adds the existing vocabulary `chat:read` and
`chat:write`. Write implies read, but read does not permit destructive mutations.
Both are existing-resource questions with required project ancestry. Grants may
be anchored at one chat, a project, or the organization. The generic tenant rule
also requires the canonical organization ancestor. The target contains trusted
resource ancestry, not a caller-submitted chat owner.

These definitions deliberately omit several capabilities:

- There are no invented `chat:blocked_read` or `chat:blocked_write` scopes; the
  current production vocabulary has neither, so `ExcludedBy` is empty.
- There is no implicit organization-admin, project, or assistant implication.
  Explicit chat grants must be assigned if the grant-based path is used.
- Chat creation is not inferred from chat write. Neither declaration supports
  `CreationOperation`, tool facts, grant constraints, or MCP discovery.
- The declaration does not assign system-role defaults or add an ownership bypass.

Once registered at client construction, the same concrete, alternative, and filter
mechanics can evaluate these scopes. For delegated execution, the grant-based path
must be allowed separately by the agent and its current delegator; a chat grant on
only one principal is insufficient. No chat-specific evaluator branch is needed.

### Ownership is a separate domain path

Current `server/internal/chat/impl.go` uses `authorizeChatAccess` for individual
chat content/mutation gates and `authorizeChatRead` for transcript and turn-stream
access. It checks project alignment and has separate handling for dashboard users,
external users, chat-session tokens, direct API keys, and managed-assistant calls.
Dashboard owners can access their own sessions; non-owners need an explicit
chat read/write grant. This is a domain ownership rule, not a scope automatically
inherited from `org:admin` or from resource ancestry.

The spike does not yet encode that ownership path. Simply returning success before
the authorization client because a user ID matches would also skip any delegated
agent bound. How ownership authority combines with agent execution needs an
explicit domain contract; it must not be inferred by the generic evaluator or by
treating owner and current delegator as interchangeable.

### Metadata and content stay distinct

Project-wide session metadata visibility does not establish permission to read
transcripts, turn streams, titles, or message bodies. A metadata-listing endpoint
needs its own documented domain gate and response projection; it must not reuse
content grants as an accidental requirement, nor expose content because a metadata
check passed. Current production chat listings and other metadata search surfaces
have their own rules and are not all interchangeable.

Pinning a shared session bookmark currently follows read, whereas rename, delete,
and feedback mutations on other members' sessions require write. Continuing an
assistant conversation also has a separate chat-ownership check. The declarations
do not implement those handlers, risk-event secret reveal, token/transport behavior,
or production listing/query semantics.

**Slice boundary:** `ChatDefinitions()` is available but is not wired into the
existing `main.go` showcase, as requested. Examples, startup registration, and
own-session versus non-owner handler flows will be reviewed individually at the
end. Package compilation/vetting checks syntax and integration with the generic
types; it is not executed evidence for new chat authorization cases.

## Agent-management mechanics: independent capabilities

[`authz/agent.go`](authz/agent.go) declares four existing management scopes without
introducing a read/write hierarchy:

| Scope             | Explicit grant-based authority     | Does not imply                              |
| ----------------- | ---------------------------------- | ------------------------------------------- |
| `agent:read`      | Inspect an agent identity          | Configuration, credentials, or transfer     |
| `agent:write`     | Configure an agent identity        | Read, credential authorization, or transfer |
| `agent:authorize` | Authorize/manage agent credentials | Read, configuration, or transfer            |
| `agent:transfer`  | Transfer ownership                 | Read, configuration, or credentials         |

The absence of implication is intentional and matches the independent expansion
entries in current `server/internal/authz/scopes.go`. These scopes also have no
registered opposing scopes; no `agent:blocked_*` names are invented. Grant
constraints, MCP discovery, tool operations, and creation forms are unsupported.

The target resource kind `Agent` answers **which agent is being managed**. It is
not the same concept as `AgentPrincipal`, which answers **who is executing**.
For example, a human may hold `agent:authorize` on one agent without inheriting
that agent's runtime MCP permissions. Conversely, an agent's runtime tool grants
do not imply authority to edit its identity or issue more credentials.

Management grants may be anchored at one agent or its organization, not a project.
An existing agent target must carry the canonical organization ancestor enforced
by generic validation; no project ancestor is required by these definitions.
Organization admin is not a cross-family implication. An administrator role may
assign all four capabilities explicitly, but the spike does not implement role
membership/default grants or infer management authority from principal kind.

### Management admission remains separate

Current `server/internal/agentmanagement/authorize.go` adds ordinary-human
authentication, active membership checks/locks, owner predicates, and mutation
coordination around scope checks. Its self-owned creation path is intrinsic, while
creating for another owner requires agent write against a prospective identity.
That behavior is not reproduced by an organization-parent creation request here.
No new creation semantics or owner bypass is inferred from `AgentWrite`.

These declarations only describe the explicit-grant part of management access.
A generic scope allow does not authorize issuance, prove grant containment, establish
human eligibility, validate a transfer recipient, or enforce lifecycle conditions.
In particular, the generic evaluator accepting an agent-principal subject is not
permission to use a human-only management endpoint. Domain admission must enforce
that boundary before mutation. Agent management and runtime agent/delegator ceilings
are distinct; no ownership policy is added to execution.

**Slice boundary:** `AgentDefinitions()` is available for later startup registration.
`main.go` is unchanged, management workflows remain explicitly out of scope, and
examples will be added individually at the end. Package vetting establishes type
compatibility, not executed evidence for creation, issuance, or transfer behavior.

## Outcomes and revocation

The backend returns `Allowed`, `Denied`, or `Unresolved`, with a reason and the
in-memory policy revision used. `Require` permits work only for `Allowed`; an error
or unresolved result cannot start protected work. Reasons describe grant matching
without exposing principal IDs or raw policy.

Every handler call reads current policy; removing the last grant does not restore
defaults. Delegator grant revocation affects the next operation through the same
bound credential. Owner lifecycle workflows remain out of scope. These examples
do not cancel already-running work, implement
distributed cache invalidation, or establish a production freshness bound.

## What is still unfinished

The code is an evolving spike, not a full implementation of every existing use case.
The remaining pieces include:

- Permission definitions and handler examples for the remaining families.
- Full plugin inventory responses and audience administration; flat OR and bounded
  two-level composition for the single-plugin metadata read gate are implemented.
- Production authentication/issuance of delegation context, scheduling, and chained
  delegation semantics; a single-level live agent/delegator bound is implemented.
- Integration with separately developed grant management: assignment containment,
  agent creation authority, and authorized grant/credential management.
- Integration with separately developed owner lifecycle handling, agent-enabled
  checks, and coordinated disablement on owner permission reduction or removal.
- Production role/directory membership admission and synchronization, principal-specific
  exception precedence, and compatibility modes; explicit fixture memberships and
  source-preserving resolution are implemented.
- Policy-driven lookup, production pagination, capabilities, and richer decision
  explanations; bounded candidate filtering is implemented.
- Restricted-use audiences and their approved authority and owner-access semantics.
- Risk-domain applicability adapters, ownership-specific domain authority, transport
  modes, lifecycle admission, public/share-token access, and staff authority.
- A replacement-backend capability/compatibility contract and production migration.

The single `main.go` showcase is intended to expand across the existing use cases,
with comments explaining what each example demonstrates. No production code has
been migrated, and no external provider compatibility is asserted.
