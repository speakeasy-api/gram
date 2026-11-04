# RBAC foundation decision log

This log records every design decision taken while implementing the Backend Revamp milestone (parent AIS-830) against the RFC "Practical RBAC: authorization foundation". Each section names its ticket and who decided its entries: approved by the reviewer, or decided without reviewer input. Each entry gives the choice and the reasoning.

Entries marked **Decided without reviewer input** were made by the implementing agent while the reviewer was away. They are open for review: each one names what would change if it is reversed. The PR that introduced a decision lists it again under "Decisions made without reviewer input".

## F3 (AIS-834): grant evaluation

Approved by the reviewer on #8434.

- Risk-policy `evaluate` compares request dimensions; the old engine ignored them. No writer stores dimensioned risk-policy grants, so no decision changes today.
- An unknown dimension key on a request is an error, not a silent skip.
- A typed permission takes its resource kind from its declared type; a caller cannot override it.
- `root` stays a built-in, not a declared relation.
- Conditions are the selector keys they constrain; they need no evaluator code.
- There is no separate grant-set type: each instance resolves to allowed or not.
- Direct-exception strictness is keyed on the request naming a `tool` dimension.

## F4 (AIS-833): facts, ownership and policy composition

### Decided without reviewer input

1. **No grant-only flag on machine policies.** The RFC says machine layers are grant-only. The evaluator keeps the actor fixed across every policy and `Is` only matches a user actor, so an agent or workload cannot use an owner's authorship in any policy. That gives the same guarantee without a flag a caller could forget to set. `TestMachineActorNeverUsesOwnerAuthorship` pins it. _If reversed:_ add a per-policy flag that disables `Is` and `Via`; no caller changes otherwise.
2. **Captured boundaries carry only policies.** A captured boundary adds its policies to the intersection. Re-checking a captured relationship is done by the caller evaluating again with the captured facts, rather than the request carrying a second fact map. _If reversed:_ add captured facts to `PolicySet` and evaluate captured policies against them.
3. **Deny plus undecidable resolves to deny.** If one policy denies and another cannot be decided, the result is a denial rather than an error. It fails closed and makes the outcome independent of policy order. _If reversed:_ `decide` returns the first error even when another policy denies; callers would see `unexpected` instead of `forbidden` in that case.
4. **Facts are supplied on the request.** The evaluator never calls a loader; `RelationshipLoader` is the contract services implement before evaluating, so batching and locking stay in services, as the RFC's responsibility table requires.
5. **No production chat or agent declarations.** The RFC's chat and agent types live only in tests until the chat and agent migration tickets declare and wire them.

Review follow-ups adopted on #8442 (each fails closed):

- Present facts must match their declared kind, so a bare presence or a value of another kind cannot satisfy a guard.
- Traversal is enforced as one hop, and related resources are validated like requests.
- An auth-client permission for a represented user other than the actor, with no represented policies, is an error. A user acting for themselves is already limited by their own live policies. No permission may reach an auth client through `Via`, so the represented ceiling always applies by the requested type.
- A standing veto block denies even when another veto check is undecided.
- An absent `bool` fact stays unresolved for `IsFalse` rather than reading as false. `bool` facts are declared for NOT NULL columns, so absence means the value was not established.

## F5 (AIS-835): typed client

### Decided without reviewer input

1. **The default registry grows one slice at a time.** `DefaultRegistry()` starts with the organization, project, MCP server and MCP auth-client types, whose compiled scopes already reproduce the frozen scope tables; those declarations moved from tests into `declarations.go`. Each migration ticket adds its own types. _If reversed:_ declaring every type up front would need every scope's semantics settled before any service moves, which the per-slice tickets exist to do.
2. **The client wraps the engine.** Admission, grant loading, the enforcement profile, challenge logging and error mapping are the engine's; only the decision moves to the registry evaluator. _If reversed:_ the client would duplicate admission, which the ticket says to reuse.
3. **No ad-hoc `RequireAny` on the client.** The client answers named alternatives the registry declares, as the RFC describes. A service that today calls `RequireAny` declares an `Alternatives` set in its migration. _If reversed:_ add `Client.RequireAny(ops...)`; callers could then combine arbitrary permissions again.
4. **Callers with neither an authenticated actor nor a loaded user identity act as `system:unattributed`.** Assistant tokens, embedded chat sessions and similar callers have neither. Their attribution never becomes a user, so ownership facts never match them, matching the RFC's rule that attribution never selects a human caller profile. _If reversed:_ those callers would match `Is(owner)` facts for their minting user.
5. **An admission with no policies denies.** The engine denies a prepared context with no policies; the client keeps that instead of returning `ErrNoPolicies`. A missing admission is still an error.
6. **Startup compiles the client; services receive it as they migrate.** `start.go` builds the default client and stops startup if the registry fails to compile. No service holds the client yet, so there is no unused injection; the first migration ticket passes it to its service.
7. **Typed constructors are not inventoried.** A consumer that calls `authz.Project(...).Read()` has migrated, so it drops out of `consumers.csv`. The inventory shrinking to wiring rows is the migration's progress signal.
8. **Typed operations take their resource kind from the declared type.** The kind-override fixture is skipped with that reason, as F3 already approved for the evaluator.

## F6 (AIS-836): cutover routing and shadow comparison

### Decided without reviewer input

1. **Units group types, and the default registry has three.** `organization`, `project` (depends on organization) and `mcp` (`mcp_server` and `mcp_auth_client`, depends on project). Routing per type or per operation would let a permission run ahead of what it traverses; routing per service would not match how the registry is declared. _If reversed:_ a slice ticket can split or merge units in `DefaultMigrationUnits()`; nothing else changes.
2. **One PostHog multivariate flag per unit, `rbac-migration-<unit>`.** This is a temporary rollout gate removed with the engine path, which the feature-flag skill assigns to PostHog rather than `productfeatures`. It is evaluated locally with the organization ID as the distinct ID, like the auth client resolver flags. _If reversed:_ a different `ModeSource` can read another store; the router does not change.
3. **A flag that cannot be evaluated means old, even for an organization already on new.** The engine is maintained and reads the same tables until X1, so falling back to it keeps access unchanged. _If reversed:_ carry the last known mode forward using `feature.DefinitionsFreshness`; a provider outage would then keep new-mode organizations on the registry evaluator.
4. **One mode per call: the most conservative of every unit touched and its dependencies.** A dependent can never run ahead of what it depends on, and one call never mixes evaluators. _If reversed:_ each operation in a call would be routed separately.
5. **Pins live on the request from admission and travel with captured boundaries.** Admission attaches an empty pin set; the first call reads a unit's flag and its dependencies' flags, and later calls, refreshes and admitted jobs reuse them. A context admission never prepared runs old. _If reversed:_ reading flags eagerly at admission would cost one flag read per unit on every request, including requests that never touch a migrated type.
6. **Shadow runs in-process, after the engine, with a bounded wait.** The engine answers first; the registry evaluator runs in a goroutine that holds one of 64 slots until it finishes, and the caller waits at most 50 ms for it. A full budget records skipped; a timeout or cancellation records failed; a panic is recovered and recorded as new_error. The response is always the engine's. _If reversed:_ an asynchronous queue would remove the wait but needs its own copy of the policies and a place to run after the request ends.
7. **Error categories are compared, not just allow and deny.** Matching categories count as a match; a changed category is new_error; an allow where the engine errored is new_allow. Only missing equivalent facts are incomparable, as the RFC defines. _If reversed:_ treating every engine error as incomparable would hide changed error categories, which the RFC's release gate asks to classify.
8. **The budget numbers are safety limits, not activation thresholds.** The ticket says activation thresholds must be agreed, not invented here. The defaults only bound cost; the exposed counters and histogram are the inputs for agreeing thresholds per unit.
9. **Records are redacted to names and categories.** They carry the organization, units, entry point, permission names, outcome, category detail, build SHA and registry fingerprint, never resource IDs, principals, grants or error messages. _If reversed:_ adding resource IDs would make single-request triage easier at the cost of putting customer resource identifiers in logs.
10. **No custom engine deciders yet.** Every typed operation must have an engine form, and `NewClient` refuses to start otherwise, because every unit starts old. The first unit with fact-only permissions (chat, agents) adds a legacy decider hook when it needs one.
11. **`MatchMany` rejects alternatives in every mode.** The engine has no batch form of alternatives and no consumer batches them, so rejecting everywhere keeps the three modes identical. _If reversed:_ old mode would need a per-option engine loop.
12. **Old-mode `Evaluate` of alternatives records a challenge.** The engine has no quiet form of `RequireAnyUnblocked`; the client calls it and turns forbidden into false. _If reversed:_ add a quiet engine helper, which changes engine code that X1 deletes.
13. **While the engine decides, a mixed `Require` records one challenge per engine call.** Single operations go through `Engine.Require` in one challenge, and each alternatives operation through `RequireAnyUnblocked` in its own, as the separate calls a service makes today do. _If reversed:_ the engine would need a combined form that X1 deletes again.
14. **A captured boundary shares the request's pins rather than a snapshot.** A unit the request never touched is read the first time the job touches it, then stays pinned for both. A snapshot could only hold the units already read, so the job would still read the rest lazily. _If reversed:_ copy the pins at capture, so the request and the job may pin different modes for a unit neither had read.
15. **Shadow recorders must not block.** The recorder runs on the request path after the bounded wait; the default one only logs and counts. _If reversed:_ the comparer hands records to a buffered channel drained by a background goroutine.
16. **One constructor builds the client for every command.** `newAuthzClient` in `cmd/gram` compiles the registry, the router and the recorder, and the API server and the worker both call it. The worker hands the client to activities through `background.WorkerOptions.AuthzClient`. Background work has no request admission, so its routes are never pinned and it runs old until a migration gives it a boundary. _If reversed:_ each command assembles the client itself, as `start.go` did at first.
17. **The inventory skips engine-named methods called through the client.** `PrepareContext`, `RefreshContext` and the other engine entry points that the inventory matches by name also exist on the client. The scan has no type information, so it decides by the receiver's name: a variable or field whose name ends in "client" is the typed client, and the call has migrated. _If reversed:_ type-check the packages during the scan, which is far slower.

## M1 (AIS-859): toolsets and the MCP server constructor

### Decided without reviewer input

1. **One MCP server constructor, `MCPServer(projectID, serverID)`, with `Read`, `Edit` and `Connect`.** Every operation carries the project as the `project_id` dimension, always, so a grant or block bounded to one project can never match a server in another. Organization is not an argument: the stored MCP grants never name it and the active organization already bounds the policy. The governance package codes against the same shape. _If reversed:_ an organization argument can be added without changing any decision.
2. **Project-level MCP operations use `MCPServersInProject(projectID)`.** Creating, cloning into or listing servers is not about one existing server, and those grants have always been stored with the project ID as the resource. The reference keeps that resource ID, so every existing grant keeps its meaning. A cleaner model is a permission anchored at the project, as the RFC's `AnchoredAt("organization", "project", Self)` describes; that needs grant storage to change and belongs after X1. _If reversed:_ a project-anchored permission replaces the reference, and stored project-resource grants need a data migration.
3. **Behaviour change: project-level and server-level MCP checks now carry the project.** The capability inventory found 42 `mcp:read`/`mcp:write` checks that used the project ID as the resource with no `project_id` dimension. Permissive matching skips grant keys a check omits, so a grant bounded to project B (`{"resource_id":"*","project_id":"B"}`) passed in project A, and a block bounded to project A was ignored. The typed constructors close both holes. In toolsets this fixes `CloneToolset` (project-level write and the source server's read, both dimensionless) and `SetToolVariationsGroup` (a dimensionless server-level write); the other toolset checks already carried the project. New frozen fixtures pin the corrected behaviour: `allow_project_grant_rejects_project_level_check_for_other_project`, `allow_project_grant_matches_project_level_check_in_its_project` and `block_project_block_applies_to_project_level_check_in_its_project`. The existing fixtures about dimensionless checks still hold, because the matcher did not change; no consumer sends a dimensionless MCP check once the M tickets land. _If reversed:_ drop the dimension from `MCPServersInProject`; the cross-project allow returns.
4. **`ListToolsets` uses `MatchMany` in place of `Filter`.** Both answer each server independently with one challenge; `MatchMany` returns answers in input order, so the handler keeps toolsets by index instead of rebuilding an ID set.
5. **The toolsets service takes the client beside the engine.** The engine still builds `auth.Auth`, as in O1.

## M2 (AIS-865): MCP server management services

### Decided without reviewer input

1. **Behaviour change: MCP endpoint checks now carry the project.** Eight of the 42 dimensionless project-level checks were in `mcpendpoints` (create, update address, hosted endpoint creation, get, list, update, slug availability, delete). They now use `MCPServersInProject(projectID)`, so a grant or block bounded to another project no longer decides them. The other services in this ticket already passed the project. _If reversed:_ as in M1, drop the dimension from `MCPServersInProject`.
2. **Organization administration checks stay organization operations.** `mcpmetadata.ServeInstallPage` and the two Okta services ask `Organization(id).Administer()`, and `ListMcpServersForOrg` asks `Organization(id).Read()`. They route with the organization unit, not the MCP unit, because that is the permission they read. _If reversed:_ an MCP-specific permission would need its own declaration and new grants.
3. **`mcpservers` and `mcpendpoints` keep the engine for shadow MCP admission.** `admission.AdministratorWriteContext` takes an engine and belongs to the shadow MCP slice. The services keep a separate `authzEngine` field for it and use the client for every decision of their own. _If reversed:_ that slice moves the admission helper to the client and the field goes away.
4. **Three list filters use `MatchMany`.** `ListMcpServers`, `ListMcpServersForOrg` and Okta's `readable` keep rows by index instead of rebuilding an ID set; the answers are the same, and a server listed twice (toolset-backed and direct with one grant ID) keeps both rows as before.
5. **Every command that builds these services builds the client with `newAuthzClient`.** `cmd/gram/mcp.go` and `private_ingress_runtime.go` now build it next to their engine, and `platformMCPConfig` carries `AuthzClient` beside `Authz`.

## M5 (AIS-863): auth clients

### Decided without reviewer input

1. **The auth-client service's own decisions move; its writers stay.** Every `authclients` handler now asks `Organization(id).Read()` or `.Administer()` through one helper that takes the operation as a method value (`authz.OrganizationRef.Administer`), and the server consent-choice list asks `MCPServer(project, server).Connect()`. The audience writers (`PatchPrincipalGrants`, `ValidatePrincipal`, `MarkAuthPermissionTakenOverTx`, `ListGrantsForResource`) write and read the grant tables, which stay authoritative until X1, so their inventory rows remain under M5, as O1 did for the skills role-grant writer.
2. **Mandatory auth-client and server decisions stay independent gates.** `mandatoryPermitter` and the mandatory shadow in `internal/mcp` use `PrepareMandatory`, `Decide` and `DecideServerAdmission`, which the RFC keeps independent of ordinary enforcement ("mandatory gates remain independent", "shadow-only gates stay shadow-only"). Routing them through the migration router would put a gate that must never be bypassed behind a per-organization flag. They keep their own fixtures and tests and stay on the engine's mandatory primitives until X1 decides where those primitives live. _If reversed:_ add a client passthrough for `PrepareMandatory` so `internal/mcp` stops naming the engine; decisions would not change.
3. **The upstream auth-client backfill stays a planner over grant rows.** `authclientbackfill` decides which grants to write by matching stored selectors offline (`GrantAllows`, `SelectorFromRow`, `UpstreamSelectorWritesAllowed`); it authorizes no request, so it has no typed-operation equivalent. It stays until X1 retires the tables it writes.
4. **No `mcp_auth_client` typed constructor yet.** The only ordinary auth-client checks left are in Platform MCP access reads, owned by another ticket, and the mandatory gates above. A constructor is added by the first consumer that asks for an ordinary `mcp_auth_client.use`, so it is shaped by a real call site. **Simplification flagged:** if no ordinary consumer appears, `mcp_auth_client` is only ever evaluated through the mandatory path, and its typed-client declaration could be reviewed at X1.

## M3 (AIS-861): MCP runtime and meta MCP

### Decided without reviewer input

1. **`MCPServerRef.CallTool(tool, disposition)` answers per-tool connection.** It is `mcp_server.connect` with the `tool` and, when known, `disposition` dimensions, as `MCPToolCallCheck` built them, plus the project, always. The RFC names it `server.CallTool(tool.Name, disposition)`.
2. **The runtime asks the client for every ordinary connection decision.** Private-server connection (hosted, proxied and meta members), per-tool filtering in `tools/list` and `tools/call`, cached member catalogs (`MatchMany`), consent inventories and token issuance now use typed operations, and the contexts they run on are prepared, refreshed and captured through the client (`PrepareContext`, `RefreshContext`, `CaptureAdmissionBoundary`), so the MCP unit's routing pins travel with the request and with code-mode jobs.
3. **Mandatory shadow inputs stay as engine checks.** Where a handler records a mandatory server or connection decision (`shadowRequire`, `shadowRequireServer`, `recordServer`), it still builds the `authz.Check` those gates take, next to the typed operation that makes the real decision. The mandatory gates are independent (see M5). **Simplification flagged:** once the mandatory primitives accept operations, the duplicated connect value in each handler goes away.
4. **Agent-consent policy evaluation stays on the grant primitives for now.** `consentPolicyConnect`, `consentAgentCandidateEligible` and the consent bindings evaluate another principal's loaded grants (`GrantsAuthorize`, `GrantsSatisfy`) to decide which agents a human may authorize; they read `agent:*` scopes, which the agents package declares, and need a client API that evaluates an explicit policy rather than the request's. They stay under M3 in the inventory until the agent types exist. _If reversed:_ add `Client.DecideWith(policy, op)` once `agent` is declared.
5. **Behaviour change: meta MCP management carries the project.** Nine of the 42 dimensionless project-level checks were in `metamcp` (create, get, list, update, delete, list/add/update/remove members). They now use `MCPServersInProject`, so a role bounded to another project no longer manages this project's meta servers. The e2e suite pinned the old cross-project allow on main for the meta MCP list.
6. **`metamcp` keeps the engine only for the shadow MCP admission writer**, as `mcpservers` does in M2.
7. **Migrated contexts leave the inventory.** The scanner skips `PrepareContext`, `RefreshContext` and `CaptureAdmissionBoundary` called through a field named `authzClient`, so the service holds the client under that name. The rows left under M3 are the agent-consent policy evaluations (item 4) and the mandatory shadow checks (item 3).

## M4 (AIS-866): remote, tunneled and unproxied MCP

### Decided without reviewer input

1. **Behaviour change: remote and unproxied MCP management carries the project.** Eighteen of the 42 dimensionless project-level checks were here: `remotemcp` (create, create with MCP server, list, get, update, probe URL, verify URL, delete, headers list/get/create/update/delete, protected-resource discovery) and `unproxiedmcp` (create, list, get, delete). They now use `MCPServersInProject`. The e2e suite pinned the old cross-project allow on main for the remote and unproxied MCP lists. `tunneledmcp` already passed the project.
2. **The remote proxy's tool interceptors take the client.** `ToolsCallAuthzInterceptor` asks `CallTool` and `ToolsListMCPConnectFilterInterceptor` asks `MatchMany` of `CallTool` operations, so per-tool decisions on proxied servers follow the MCP unit like hosted ones. `ProxyManager` holds the client instead of the engine, and the workload reconnect interceptor in `internal/mcp` passes the client too.
3. **Scope pinning names operations, not scopes.** `authorizeServerScopes` takes `authz.MCPServerRef.Read` or `.Edit` as a method value, as `authclients` does for organization operations; the sharing-server checks are lists of `Edit` operations.
4. **`remotemcp` keeps the engine only for shadow MCP admission**, as `mcpservers` does.
