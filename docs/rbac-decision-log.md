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
18. **Loaded-grant questions route like every other decision.** `Client.EvaluateGrants(ctx, grants, op)` answers whether an already-loaded grant set allows an operation, such as another principal's or a role's grants. It routes old, shadow or new by the operation's unit, records no challenge (as `GrantsAuthorize` records none today), and rejects alternatives. It was first written for remote sessions and moved here so every slice shares one entry point. _If reversed:_ each slice keeps `GrantsAuthorize` until X1.
19. **The client exposes `ShouldEnforce`.** It forwards to the engine, so a migrated service that skips filtering for unenforced requests no longer needs the engine for that one question. _If reversed:_ such services keep an engine field until X1.

## O1 (AIS-837): organizations and product features

### Decided without reviewer input

1. **Services receive the client beside the engine.** `organizations.NewService` and `productfeatures.NewService` take both. The engine is still needed to build `auth.Auth` and to check a requested organization other than the active one, which `RequireUserOrganizationScope` answers from that organization's live grants and which no typed operation covers yet. _If reversed:_ expose the engine from the client and pass only the client; the requested-organization path would then reach through it.
2. **A conditional `Require` stays a `Require`.** Three handlers ask for administration and fall back to read: the webhook portal picks its capabilities, and setup tasks decide whether assignment is allowed. They keep `Require` rather than `Evaluate`, so challenge records stay exactly as they are today. _If reversed:_ switch them to `Evaluate`; a non-admin would no longer leave a denied challenge for the admin check.
3. **Product features maps its scope parameter to an operation locally.** `authorizeOrganization` serves the active and the requested organization with one scope argument. A small switch maps it to `Organization(id).Read()` or `.Administer()`, and any other scope becomes the zero operation, which the client rejects as unexpected. _If reversed:_ pass a method value such as `authz.OrganizationRef.Administer` and keep the scope only for the requested-organization path.
4. **The skills role-grant writer stays.** `provisionSkillsSystemRoleGrantsTx` writes role grants through `PatchRoleGrantsTx`, which the migration plan keeps until X1 retires the tables, so its inventory row remains under O1.
5. **Parity is proven by the service's own tests in shadow.** `authztest.NewParityClient` puts every unit in shadow and fails the test on any disagreement. The organizations and product-features suites made 419 comparisons, all matching.

## O3 (AIS-839): identity, directory and device integrations

### Decided without reviewer input

1. **Notification audiences go through the client.** `Client.OrganizationAdministratorEmails` replaces direct calls to `ResolveOrganizationAdminEmails`. Each member's live grants become one policy, and the decision is `Organization(id).Administer()`. The engine helper stays as the old path, and X1 deletes it. _If reversed:_ audiences keep reading the old tables until X1, and no shadow evidence exists for them.
2. **Audiences route by the organization, not by a request.** Background work has no admission, so `routeForOrganization` reads the organization unit's mode, and its dependencies' modes, for that organization on every call and pins nothing. One resolution is one boundary. _If reversed:_ background audiences would always run old until the engine is removed.
3. **Audience shadow runs inline and unbudgeted.** Grants are read once per member, and both evaluators answer from the same grants. The comparison is recorded as entry `audience`, with one answer per member. The registry evaluator does no I/O, so no timeout or slot applies. _If reversed:_ the comparison runs in a goroutine under the budget like request calls, which costs a second copy of every member's grants.
4. **A worker without a client resolves with the engine.** Test workers and commands that build no client keep the old behaviour. That fallback row is owned by X1 in the inventory. _If reversed:_ every worker would have to build a client, including test helpers.
5. **The identity-provider `authorize` helper takes an operation, not a scope.** Callers pass `authz.OrganizationRef.Administer` or `authz.OrganizationRef.Read`, so no scope is mapped locally. _If reversed:_ map the scope as product features does.
6. **Device-integration handlers have no service tests.** Their thirteen checks are organization read and administer on the active organization, which the client and shadow corpora already cover. No new evidence was added for them.

## O4 (AIS-844): external credentials, keys, session issuers and key sets

### Decided without reviewer input

1. **Shared access helpers take an operation.** `requireOrgAccess`, `authorize` and `access` now take `func(authz.OrganizationRef) authz.Operation`, and callers pass `authz.OrganizationRef.Administer` or `.Read`, as identity-provider connections do in O3. _If reversed:_ map scopes locally, as product features does.
2. **One remote-sessions test builds its key-set service with a parity client.** The delegation-status test constructs a `jsonwebkeysets` service directly, so it now passes a client too. That is the only change outside O4's packages.
