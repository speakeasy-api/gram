# RFC: An authorization client with replaceable backends

**Proposed design reference — do not merge.** The code beside this RFC is an
isolated memory-backed exploration, not a production implementation or an approved
policy migration. Production delivery starts separately from a verified baseline.

The Overview below is preserved from the earlier RFC. In the rest of this document,
we call the application-facing interface the **client**. Code paths are relative to
this directory unless they begin with `server/`.

## Overview

Our authorization system represents permissions as grants and checks those grants when a caller performs an operation. Although that basic model is useful, callers must understand too much of its implementation: handlers construct resource selectors themselves, while listings, dashboard controls, and delegation code interpret overlapping permission rules. Those implementations can disagree, allowing an incomplete check to permit access that a complete check would deny.

We propose making `authz` the stable frontend—the Go application-facing interface—through which application code asks authorization questions, with a backend that implements those contracts. Handlers will name an operation and provide a typed target built from trusted resource data, while shared permission and resource definitions describe supported grant boundaries, implications, and attributes. Through this interface, callers can authorize an action, find accessible resources, discover whether any access remains, explain a decision, or prove that delegated permissions fit within their parent policies without depending on the backend's storage or evaluation APIs.

We also propose changing how we represent restricted MCP access. Today, broad default grants make servers accessible, resource-specific blocks narrow that access, and direct-user exceptions restore selected access, creating precedence rules that several consumers must reproduce. For explicitly migrated servers, an audience will instead supply permission to use the server, subject to the execution mode's independent policy bounds and mandatory prohibitions. Management access will not accidentally confer execution through permission expansion. Administrator execution, audience interactions, and migration of existing credential restrictions remain decisions for review.

The initial backend will retain our Go evaluator and Postgres, and a later backend can replace it only after demonstrating the same policy, lookup, delegation, and consistency contracts; a common constructor alone does not establish compatibility. We will deliver confirmed guard and revocation fixes first, followed by the frontend, initial backend, and typed caller migration before the restricted-use pilot, while giving risk-policy applicability a separate domain contract over existing storage. This approach requires temporary compatibility code, but lets us correct unsafe behavior without forcing every resource and policy through a single migration.

The [TLDR / Key Decisions](#tldr--key-decisions) is the change summary for reviewers. The [Proposal](#proposal) defines the contracts; [Rollout and Validation](#rollout-and-validation) retains the implementation phases and exit criteria. [Appendix A](#appendix-a--rbac-use-cases-and-migration-coverage) maps 27 existing use cases to the changes and acceptance cases.

## Goals

We want developers to ask authorization questions without reconstructing policy
rules. Equivalent list, detail, invocation, and capability checks should agree for
the same identity and policy state. Missing facts must not widen access, and
removing access must remain effective.

The initial production backend will retain Go and Postgres. This proposal does not
require an external authorization service, new storage, or changes to every role.
Public links, ownership, staff access, and workload admission retain explicit
domain rules rather than becoming implicit grant bypasses.

## TLDR / Key Decisions

- Define permission meaning once: supported resources, grant boundaries,
  implications, opposing scopes, constraints, and request forms.
- Construct one `authz.Client` at startup. Handlers submit struct requests and
  targets adapted from the resource rows they already load.
- Separate trusted identity and execution admission from policy resolution and
  evaluation. Preserve grant origins and load independent policies coherently.
- For an agent acting for a user, require both agent and actual current delegator
  authority. Never infer the delegator from ownership or merge their grants.
- Keep concrete checks, connection admission, candidate filtering, and assignment
  containment distinct. Each answers a different question.
- Preserve existing semantics until an explicit migration approves a change.
  Audiences, mandatory prohibitions, credential limits, and OR composition still
  need decisions; the reference is not evidence of production compatibility.

## Proposal

### 1. Describe permissions in a shared registry

We propose domain-owned permission definitions aggregated by `AllDefinitions()`.
The client copies these declarations into a registry at startup. Definitions
describe what grants mean; they assign no grants and restore no defaults.

The MCP connection definition illustrates the contract implemented by the reference:

```go
authz.PermissionDefinition{
    Name:               authz.MCPConnect,
    Resource:           authz.MCPServer,
    AllowedAnchors:     []authz.ResourceKind{authz.MCPServer, authz.Project, authz.Organization},
    RequiredAncestors:  []authz.ResourceKind{authz.Project},
    ImpliedBy:          []authz.Permission{authz.MCPRead, authz.MCPWrite},
    ExcludedBy:         []authz.Permission{authz.MCPBlockedConnect},
    AllowedConstraints: []authz.ConstraintKind{authz.ToolConstraint, authz.DispositionConstraint},
    Forms:              []authz.OperationForm{authz.ToolOperation},
    Discoverable:       true,
}
```

An **anchor** is where a grant applies: one server, a project, or an organization.
An **ancestor** describes where the actual resource belongs. Containment is not
permission implication: a server belonging to a project does not make
`project:read` imply `mcp:connect`. Implications are explicit and non-transitive.

Opposing permissions remain named scopes, such as `mcp:blocked_connect`; grants
have no new allow/deny effect field. Constraints narrow a grant, for example to
one tool or read-only operations. An empty `AllowedConstraints` permits no
constrained grants for that permission.

See [permission types](authz/permission.go), [MCP definitions](authz/mcp.go), and
the [definition catalog](authz/definitions.go). The catalog covers the modeled
families, not every production permission. Generating or checking dashboard and
API metadata against it remains production work.

### 2. Inject one client and reuse ordinary resource loading

Startup constructs the shared client and injects it into domain services:

```go
client, err := authz.NewClient(backend, authz.AllDefinitions())
if err != nil {
    return err
}
// Inject client into project, MCP, assistant, and other services.
```

Handlers choose a permission, not a backend or a subset of definitions. A domain
adapter's `AuthzTarget()` supplies identity and ancestry from trusted resource
data. It need not add methods to generated database types or perform an extra
reference-only query.

The project detail example reuses its ordinary tenant-scoped lookup:

```go
project, err := s.projects.Get(ctx, subject.Principal.OrganizationID, projectID)
if err != nil {
    return Project{}, err
}
if err := s.authz.Require(ctx, subject, authz.Request{
    Permission: authz.ProjectRead,
    Target:     project.AuthzTarget(),
}); err != nil {
    return Project{}, err
}
return project, nil
```

This signature exists in the reference. The subject must come from trusted
authentication, not a request payload. A context-based convenience may later omit
the explicit subject, but it is not implemented and its naming remains open.

The client validates shape and tenant consistency. It cannot prove that asserted
ancestry is true. Tenant-scoped repository loading remains responsible for that
truth, and protected content or side effects must wait for authorization. See the
[project handlers](usecases/projects/list.go) and [MCP adapter](usecases/mcps/invoke.go).

### 3. Establish identity before resolving policy

Authentication establishes the actor, organization, and actual delegator, if any.
Execution admission then establishes whether that identity may use this execution
mode: a structurally valid subject is not proof of an active session, agent, or
credential. Resolution loads the eligible policy sources; evaluation answers the
operation's question.

```text
trusted execution identity → admission → coherent policy resolution
                          → evaluation → protected work only on explicit allow
```

These are responsibilities, not required database round trips. The reference
client validates the request first; the memory backend admits and resolves under
one lock. Production can coordinate the equivalent reads in one transaction.

The reference separates `Principal`, `Authentication`, and `Delegation` within a
`Subject`. Its credential bindings reject substituted delegators and direct-agent
bypass. They are fixtures, not authentication or issuance: secret verification,
expiry, lifecycle, active membership, and proof of the real invoker remain outside
the exploration. See [subjects](authz/subject.go), [admission](authz/admission.go),
and [delegation](authz/delegation.go).

### 4. Resolve grants with their origins intact

A user's policy combines eligible direct assignments, explicitly assigned roles,
and organization-wide user assignments. An agent receives its own assignments and
roles, not its owner's roles or `user:all`. Reading empty policy grants nothing;
provisioning defaults is a separate durable write.

We retain each grant's origin because a direct assignment and an inherited grant
can have different precedence. The reference exposes these resolution types:

```go
type ResolvedGrant struct {
    Source GrantSource
    Grant  Grant
}

type ResolvedPolicy struct {
    Principal Principal
    Grants    []ResolvedGrant
}

type PolicyResolution struct {
    Policies []ResolvedPolicy
    Revision uint64
}
```

`Policies` keeps independently required principals separate. `Revision` identifies
one memory snapshot, not a production consistency token or reusable authority.
Source ordering makes iteration deterministic; it does not define precedence.

`PolicyResolver.ResolvePolicies(ctx, subject)` admits execution and resolves its
policies coherently. The memory backend copies bindings, memberships, and grants
under one lock. A database backend must provide equivalent consistency rather than
combine unrelated snapshots. See [policy contracts](authz/policy.go) and the
[memory resolver](authz/memory_policy.go).

Detailed production grant-loading work is parked. We propose preserving this
contract while reusing adequate existing queries, not replacing SQL for its own
sake. Transaction isolation, invalid-row handling, membership freshness, and
request-local reuse still need a bounded integration design.

### 5. Evaluate the complete operation within each required policy

For one policy, the evaluator matches the requested permission or an explicit
implication, the grant anchor, and every populated constraint against trusted
facts. Matching opposing scopes remove access under the selected policy model.
Missing facts cannot prove a constrained allow or justify ignoring a potentially
applicable block. Unknown tool disposition therefore cannot establish read-only
access, although an unrestricted grant need not require classification.

`Authorize` returns `Allowed`, `Denied`, or `Unresolved`. `Require` permits work
only for `Allowed`. Backend errors also stop work, but remain distinguishable from
policy denials for diagnostics. RBAC does not replace existence, entitlement,
quota, risk, or upstream credential checks.

For on-behalf-of execution, the proposal evaluates the agent and actual current
delegator independently and requires both to permit the operation. Neither policy
can fill a gap in the other. Ownership is a management/lifecycle relationship, not
proof of who invokes the agent.

This direction does not authorize dropping existing restrictions. The inspected
principal-credential path also uses immutable credential limits and live owner
policy; workloads use immutable session limits and an assigned agent's live
policy, without an owner's runtime grants. The reference has no credential grant
collection or workload implementation. Their migration remains a separate decision.

Composition must also be explicit:

| Client method         | Meaning                                                             |
| --------------------- | ------------------------------------------------------------------- |
| `RequireAny`          | Any permitted branch is sufficient; exclusions stay within a branch |
| `RequireAnyUnblocked` | Alternatives share exclusions protecting the same operation         |
| `RequireAnyGroups`    | Independent paths retain their own alternative/exclusion boundaries |

The reference evaluates the whole expression within each required policy, then
intersects the results. Inspected production composition requires a common branch
across policies. If one policy permits only X and another only Y, these contracts
differ. We must settle that difference before migration. General `RequireAll` and
arbitrary recursive expressions are not implemented here.

### 6. Keep filtering, admission, and execution distinct

`Filter` evaluates a bounded collection of already-loaded targets for one
permission. It preserves order, rejects malformed or duplicate candidates, and
returns only explicit allows under one coherent resolution:

```go
allowed, err := s.authz.Filter(ctx, subject, authz.ProjectRead, targets)
if err != nil {
    return nil, err
}
// Return only the matching rows, preserving repository order.
```

The 100-candidate bound limits the demonstration. This is not database lookup or
authorized pagination. Filtering one arbitrary database page cannot establish the
end of visible results or justify unfiltered totals. Production lookup must define
scanning limits, cursors, totals, and hidden-resource behavior separately.

Connection admission asks whether a common nonempty permitted region remains
after exclusions and every independent bound. It does not invent a tool call:

```go
decision, err := s.authz.DiscoverAccess(ctx, subject, authz.MCPConnect, server.AuthzTarget())
if err != nil {
    return err
}
if decision.Outcome != authz.Allowed {
    return authz.ErrDenied
}
return s.runtime.Open(ctx, server)
```

This policy discovery neither contacts upstream nor proves a matching tool exists.
Disjoint agent and delegator policies cannot admit a connection merely because
each is nonempty. Admission is not authority for later calls: every invocation
checks concrete trusted tool facts before upstream execution:

```go
if err := s.authz.Require(ctx, subject, authz.Request{
    Permission: authz.MCPConnect,
    Target:     server.AuthzTarget(),
    Tool:       &facts,
}); err != nil {
    return "", err
}
return s.runtime.Call(ctx, server, facts.Name)
```

These APIs exist in the reference [MCP service](usecases/mcps/invoke.go). Catalog
classification accuracy, routing stability, transport binding, and public access
are separate production obligations. Assignment containment is another distinct
question: proving that an entire proposed policy fits its delegable parents. A
successful invocation or resource lookup cannot substitute for that proof.

### 7. Separate evaluation from policy administration

`Backend` evaluates questions. Optional `PolicyWriter`, `RoleAssignmentWriter`,
and `CredentialWriter` capabilities persist fixture changes. A backend's ability
to write policy does not authorize the editor. Grant containment, audience-edit
authority, human-only management, lifecycle checks, and audit evidence belong to
the domain workflow.

`ReplaceGrants` replaces one direct source, not authority inherited from roles.
Credential binding records an asserted identity relationship, not secure issuance
or permission to invoke an agent. Production writes must preserve atomic resource
and policy changes, conflicting-write protection, and read-after-revocation
guarantees. Unsupported backend capabilities must fail closed, and production
configuration must reject a backend missing required operations.

### User Experience

The exploration changes no product surface. A later migration should make lists,
controls, capability responses, and server decisions agree without letting UI
state authorize work. Explanations must not reveal hidden resources or unrelated
policy. Platform MCP parity must be assessed alongside each migrated workflow;
there is no production API change in this reference.

Restricted MCP audiences remain a separate product migration. We recommend
separating ordinary use eligibility from mandatory prohibitions, but neither that
precedence change nor removal of management-to-execution implications is approved
or implemented here. Exactly one selected model must govern each resource.

### Billing

The reference adds no billing or entitlement behavior. Production authorization
allows must still pass the existing entitlement, quota, and metering paths; this
exploration has not verified those integrations.

### Threat Modeling

The main risks are forged identity or ancestry, lost grant provenance, authority
assembled across independent policies, missing facts treated as permission, and
stale admission after revocation. Trusted adapters, explicit bounds, fail-closed
outcomes, and coherent reads address parts of that design. Real authentication,
distributed freshness, credential migration, and lifecycle enforcement remain
unproven. Error and explanation responses must avoid exposing protected content.

## Alternatives Considered

Keeping separate helpers is less disruptive, but leaves callers responsible for
matching, discovery, and precedence. We prefer one client contract so differences
become explicit and testable. The cost is temporary adapters and compatibility
code while callers migrate.

Replacing Postgres or adopting a relationship engine immediately could improve
some lookup patterns, but would not settle tool facts, delegation, or precedence.
It also adds consistency work where resource and grant writes share transactions.
We prefer to establish the contract first. A future backend must prove behavioral
and consistency compatibility, not merely implement the same constructor.

## Rollout and Validation

The draft PR is a non-production reference. None of the following phases is
completed by compiling it:

1. **Establish the baseline.** Verify production behavior and fix confirmed guards
   or revocation defects independently. Capture compatibility cases before moving
   callers. Keep private investigations out of the public reference.
2. **Introduce the client.** Add shared definitions, trusted target adapters, and
   the Go/Postgres backend. Establish transaction, admission, and freshness
   contracts. Move a small domain without changing its policy semantics.
3. **Migrate consumers.** Align concrete checks, lists, capabilities, and
   assignment containment. Cover tenant boundaries, source precedence, missing
   facts, delegated execution, and equivalent API/MCP behavior.
4. **Migrate execution modes.** Preserve lifecycle and credential restrictions,
   establish actual delegators, and define revocation bounds for requests,
   connections, streams, queued work, and caches. Workloads retain their distinct
   contract. Settle OR semantics before migrating composed checks.
5. **Pilot restricted use separately.** Approve audience authority, administrator
   execution, and prohibition semantics before changing a resource's policy model.
   Validate both migration and rollback without unioning old and new access.

Keep old adapters until equivalent decisions are demonstrated. Rollback must
restore the selected policy representation and admission contract, not bypass
checks or widen access. Role/default redesign, role leases, sensitive-content
permissions, risk applicability, and external providers remain separate workstreams.

The reference has runnable self-checking scenarios, not test files or production
integration tests. Run these from the repository root:

```sh
mise exec -- go build ./examples/authz-spike/...
mise exec -- go vet ./examples/authz-spike/...
mise exec -- go run ./examples/authz-spike/usecases/skills/blocking
mise exec -- go run ./examples/authz-spike/usecases/projects/visibility
mise exec -- go run ./examples/authz-spike/usecases/mcps/toolaccess
```

The original `main.go` remains a historical showcase and has not been rewritten
to serve as migration evidence. The [use-case guide](usecases/README.md) describes
the focused examples and their limits. Real authentication, database transactions,
legacy inherited-block exceptions, workloads, production paging, and distributed
revocation are not established by these runs.

## Open Questions

The final section lists the unresolved business decisions. Production integration
also needs answers to these engineering questions:

- What context-based convenience should complement the explicit-subject API?
- Which existing queries and transaction boundaries meet coherent admission and
  resolution requirements? Detailed grant-loading redesign remains parked.
- How should invalid stored policy be rejected or quarantined without dropping
  restrictions while retaining allows?
- What lookup scanning limits, pagination guarantees, and explanation redaction
  rules must every production backend support?
- Which backend capabilities are mandatory at startup, and how will resource and
  policy writers coordinate atomic changes?

## Appendix A — RBAC use cases and migration coverage

This compact inventory preserves the earlier 27 use cases as review obligations,
not completed coverage. The business rules below state their required behavior.

| Use case                              | Migration or acceptance obligation                                 |
| ------------------------------------- | ------------------------------------------------------------------ |
| Baseline member access                | Provision explicitly; final-grant removal stays effective          |
| Roles and memberships                 | Preserve eligible sources and live membership changes              |
| Project visibility                    | Equivalent list and detail decisions; tenant isolation             |
| MCP configuration                     | Distinguish create, inspect, and edit permissions                  |
| Private MCP connection                | Admit only a common permitted policy region                        |
| Limited tool execution                | Check trusted tool facts on every invocation                       |
| Team restrictions and exceptions      | Preserve source-aware precedence until migrated                    |
| Restricted-server administration      | Decide management versus execution explicitly                      |
| Accessible resources and explanations | Align visibility, paging, redaction, and access requests           |
| Environments                          | Protect secrets and both sides of attach/clone operations          |
| Skills                                | Keep read/write blocks and discovery consistent                    |
| Assistants                            | Separate configuration from runtime interaction authority          |
| Chats                                 | Preserve ownership and explicit non-owner content permissions      |
| Session metadata and telemetry        | Do not treat metadata visibility as content access                 |
| API, function, and image uploads      | Check the actual parent/resource authority                         |
| Plugins                               | Distribution does not grant member editing or execution            |
| Agent management                      | Retain independent scopes and human-only admission                 |
| Delegated credentials                 | Preserve narrowing and require the actual delegator                |
| Workloads                             | Enforce trust admission, session limits, and assigned-agent policy |
| Device sync and hooks                 | Use explicit machine admission and ingestion authority             |
| Risk and server approval              | Keep applicability and bypass separate from RBAC grants            |
| Legacy and sessionless credentials    | Preserve each mode's explicit authority and limits                 |
| Platform MCP                          | Match the equivalent management workflow's authorization           |
| Public resources and capability links | Bound the authority to the intended operation                      |
| Staff and support                     | Separate tenant administration from audited support authority      |
| Organization settings and exports     | Protect sensitive mutations and destinations                       |
| Dashboard controls and audit evidence | Describe server decisions; never replace enforcement               |

## RBAC business rules

These rules describe required access behavior across APIs, MCP tools, streams,
and background work. Their examples are requirements, not claims of verification.

| Area                    | Business rule                                                                                                                                                                    | Example                                                                                         |
| ----------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------- |
| Access                  | Protected work requires effective permission or an explicit domain rule.                                                                                                         | Own-chat access uses its ownership rule, not a universal grant bypass.                          |
| Failure                 | Denied, unresolved, and failed checks must not start protected work.                                                                                                             | A policy lookup failure starts no job or upstream call.                                         |
| Identity                | Authentication establishes actor, organization, and anyone represented. Attribution alone grants no authority.                                                                   | A payload user ID cannot establish the delegator.                                               |
| Identity kinds          | Users, agents, and workloads are distinct even when IDs match.                                                                                                                   | User A's grants do not become Agent A's grants.                                                 |
| Organization boundaries | Grants, resources, and delegation stay within the authorized organization.                                                                                                       | Knowing another organization's resource ID grants no access.                                    |
| Resource boundaries     | Trusted data establishes actual organization and project.                                                                                                                        | A caller cannot present Server B as belonging to Project A.                                     |
| Request validation      | Unknown permissions, malformed targets, missing required facts, and unsupported operations cannot widen access.                                                                  | A wildcard grant may cover a server; `*` is not an executable server ID.                        |
| Grant sources           | Users receive only eligible direct, role, and all-user assignments; machines do not inherit human grants automatically.                                                          | An agent does not automatically receive `user:all`.                                             |
| Implications            | Permissions imply only explicitly declared permissions.                                                                                                                          | MCP write implies connect; agent write does not imply agent authorize.                          |
| Permission boundaries   | Organization/project administration is not a universal bypass.                                                                                                                   | Project read cannot reveal environment secrets or invoke tools.                                 |
| Blocks                  | A matching block removes access, including broader allows, except for the direct-user exception below.                                                                           | A server block defeats project-wide access there, not on sibling servers.                       |
| Direct-user exceptions  | A concrete direct-user allow can override inherited role/all-user blocks within its reach, not the user's own block. Wildcard allows and agent grants receive no such exception. | A direct allow on Server A does not override Server B's block.                                  |
| Blocking scope          | MCP connect, read, and write blocks are independent; other families use declared relationships.                                                                                  | Environment read block also blocks write; write block alone can retain read.                    |
| Required permissions    | Every permission required by the operation must be satisfied.                                                                                                                    | Audience reads require MCP read and organization read.                                          |
| Alternatives            | A complete permitted alternative suffices; exclusions are shared only where the operation explicitly requires it.                                                                | An administrative fallback cannot escape shared exclusions.                                     |
| Grouped alternatives    | An exclusion in one group must not veto an independent path.                                                                                                                     | Organization-read plugin metadata access is separate from its write/admin fallback.             |
| Independent policies    | Never merge required policies to manufacture authority.                                                                                                                          | Agent grants cannot fill gaps in delegator policy.                                              |
| Constraints             | Every populated grant constraint must match and be supported by the permission.                                                                                                  | A read-only `list` grant does not permit destructive `list` or `delete`.                        |
| Missing facts           | Missing facts prove neither a constrained allow nor an exclusion's irrelevance.                                                                                                  | Unknown classification is not read-only; an unrestricted grant need not require classification. |
| Tool facts              | Use trusted classifications; changes affect later checks within the freshness bound.                                                                                             | Reclassifying a tool as destructive removes read-only eligibility.                              |
| Connection admission    | Require common remaining access; admission neither authorizes every tool nor proves a tool exists.                                                                               | Disjoint tool-limited policies cannot jointly admit a connection.                               |
| Lists                   | Return only permitted resources; equivalent list/detail checks agree.                                                                                                            | A read-blocked skill is absent from the readable list.                                          |
| Pagination              | Totals, facets, cursors, and continuation respect visibility.                                                                                                                    | Hidden rows neither appear in totals nor incorrectly terminate scanning.                        |
| Batch filtering         | Use coherent policy, preserve order, reject malformed candidates, and return only explicit allows.                                                                               | A batch cannot mix grants and exclusions from incompatible snapshots.                           |
| Content                 | Metadata authority does not imply transcript, stream, tool-result, or secret access.                                                                                             | Session metadata access alone cannot download a transcript.                                     |
| Diagnostics             | Explanations and errors must not reveal hidden resources, unrelated grants, or secrets.                                                                                          | A refusal does not enumerate inaccessible resource names.                                       |
| UI                      | Controls and capabilities describe access; the server rechecks the operation.                                                                                                    | A previously enabled button cannot defeat revocation.                                           |
| Delegation              | On-behalf-of work requires both agent and actual current delegator authority.                                                                                                    | Neither a powerful agent nor a powerful invoker can replace the other's missing permission.     |
| Delegator identity      | Ownership and issuance do not prove the current invoker.                                                                                                                         | A shared credential naming a user does not authenticate every external invoker as that user.    |
| Delegation validation   | Reject substituted, cross-organization, or unsupported contexts.                                                                                                                 | Omitting delegation cannot turn an agent into unrestricted direct execution.                    |
| Workloads               | Enforce immutable session limits and assigned-agent live policy, not owner or all-user grants.                                                                                   | A privileged owner cannot widen a workload session.                                             |
| Lifecycle               | Grants do not make inactive identities or credentials usable; execution and refresh retain eligibility checks.                                                                   | A suspended agent cannot execute through surviving grant rows.                                  |
| Grant assignment        | Valid syntax does not authorize editing; assigned policy must fit applicable delegation limits.                                                                                  | A well-formed write grant still requires assignment authority.                                  |
| Credentials             | Management, issuance, and invocation are separate authorities.                                                                                                                   | A valid binding alone authorizes neither issuance nor invocation.                               |
| Chat ownership          | Own-chat access is explicit; non-owner reads and destructive mutations require their respective permissions.                                                                     | Chat read does not permit deleting another member's session.                                    |
| Chat transports         | Loading and streaming enforce equivalent project/ownership boundaries for the same mode.                                                                                         | User A's chat token cannot stream User B's chat in the same project.                            |
| Agent management        | Read, write, authorize, and transfer are independent; human-only operations require eligible humans and retain nondelegable owner capabilities.                                  | User attribution on an agent credential does not admit it as a human.                           |
| Plugins                 | Distribution does not authorize editing referenced resources or executing members.                                                                                               | Plugin write alone cannot alter a referenced server's tools.                                    |
| Audiences               | Audience access has separate authority: reads need MCP and organization read; writes need organization administration.                                                           | MCP write or agent ownership alone cannot enroll principals.                                    |
| Audience safeguards     | Preserve self-lockout protection and restrictions retained by suspended/revoked agents.                                                                                          | Re-saving a suspended agent's limited rule cannot make it unrestricted.                         |
| Creation                | Check the actual parent or prospective-resource permission.                                                                                                                      | Project creation requires organization administration, not child write alone.                   |
| Multiple resources      | Authorize every required source and destination.                                                                                                                                 | Destination write alone cannot read source environment secrets.                                 |
| Risk                    | Applicability, exemption, and blocking have separate semantics; errors are not exemptions.                                                                                       | `risk_policy:block` is not a generic opposing access scope.                                     |
| Other authority         | Public links, external users, legacy keys, staff/support, and ingestion require explicit limits.                                                                                 | Missing a human session is not itself authority; links cannot mutate unrelated resources.       |
| Revocation              | Reads, sign-in, and default reconciliation must not restore deliberately removed grants.                                                                                         | Removing final assistant access stays effective.                                                |
| Policy writes           | Authorize separately, change only intended rules, and serialize or reject conflicts.                                                                                             | A stale audience replacement cannot erase a concurrent change.                                  |
| Consistency             | Each decision uses mutually consistent identity, delegation, and policy state; backend replacement preserves that contract.                                                      | No allow assembled from incompatible snapshots.                                                 |
| Freshness               | Define revocation limits for requests, refresh, connections, streams, queued work, and caches.                                                                                   | Existing connections recheck later protected operations.                                        |
| Invalid policy          | Invalid stored policy cannot broaden access.                                                                                                                                     | Do not discard a malformed block while retaining its broad allow.                               |
| Surface consistency     | Equivalent API, SDK, list, dashboard, and Platform MCP questions follow the same rules.                                                                                          | A UI scope shortcut cannot override a server denial.                                            |
| Other prerequisites     | RBAC does not replace entitlement, quota, risk, existence, or upstream authority.                                                                                                | An RBAC allow cannot supply missing upstream credentials.                                       |
| Migration               | Select one model per resource; preserve future membership behavior and a recovery path.                                                                                          | Today's role-member list is not equivalent to a live role grant.                                |

### Business questions still to settle

- Should concrete direct-user exceptions remain, or should selected resources use
  mandatory prohibitions that ordinary grants cannot override?
- How do immutable credential limits and live owner restrictions migrate to
  actual-delegator checks without widening authority? What bounds autonomous work?
- Must policies permit a common alternative, or may each permit a different valid
  alternative for the complete operation?
- How does own-resource access combine with an agent lacking an explicit grant?
- Who may see project-wide session metadata, including administrators, and what
  extra permission protects content in telemetry logs?
- For restricted MCP use, who supplies use authority, does management imply
  execution, and what metadata remains visible outside the audience?
- May single-server editors upload images, or is project-wide authority required?
- Which privileged overrides remain, and what audit and recovery rules apply?
- What are the numeric revocation limits, including already-running work?
