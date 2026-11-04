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

## P1 (AIS-843): projects

### Decided without reviewer input

1. **Services receive the client beside the engine.** `projects.NewService` takes both, as O1 does: the engine still builds `auth.Auth`, whose checks A1 migrates. _If reversed:_ expose the engine from the client and pass only the client.
2. **`ListProjects` filters with `MatchMany` and zips the results.** The client has no `Filter`; the handler builds one `Project(org, id).Read()` per listed project and keeps the projects whose answer is true. The engine form is `FindMatched`, which logs the same `filter` challenge as `Filter` did, and an empty list still reaches the client so a missing admission keeps erroring. _If reversed (simplification):_ add `Client.Filter(ctx, ops) ([]string, error)` returning allowed resource IDs; about a dozen list handlers across the slices zip `MatchMany` the same way.
3. **The whitelist setter administers the target organization, not the active one.** `SetOrganizationWhitelist` is a Speakeasy-team action on another organization; it keeps checking administration of `payload.OrganizationID`, now as `Organization(payload.OrganizationID).Administer()`. `Organization`'s contract says the caller has verified the organization belongs to the tenant; here the guard is the team-organization check above it. _If reversed:_ check administration of the active (team) organization, which is a behaviour change and needs its own approval.
4. **Parity is proven by the service's own tests in shadow.** `authztest.NewParityClient` fails the test on any disagreement. The projects suite made 37 comparisons, all matching.

## P2 (AIS-851): deployments, packages and templates

### Decided without reviewer input

1. **A pure project-read and project-write slice.** Every check in the three services is `Project(org, id).Read()` or `.Write()` on the request's project, so nothing new is declared and the `project` unit routes them all. _If reversed:_ deployments could get their own `deployment` type, but no stored grant names a deployment today, so it would only rename `project:write`.
2. **Tests of services that build these services also use the parity client.** The tools, toolsets and functions suites construct the deployments, packages and templates services, so their decisions are compared too. _If reversed:_ give those suites `authztest.NewClient`; their decisions would still be engine-only and uncompared.
3. **Templates keeps building its variations service from the engine.** `variations` belongs to P3, which switches that constructor. _If reversed:_ none; P3 does it.
4. **Parity:** the deployments, packages, templates, tools, toolsets and functions suites made 384 comparisons, all matching. Three tests fail identically on the base branch in this environment (two toolsets OAuth-issuer tests that need outbound network, one functions local-runner test that refuses to run as root), unrelated to this change.

## P3 (AIS-847): tools, resources, variations, external MCP catalog and registry discovery

### Decided without reviewer input

1. **Ten of the eleven checks move now; the project-wide MCP write stays on the engine.** Every project read and write in tools, resources, variations, externalmcp and registry discovery becomes `Project(org, id).Read()` or `.Write()`. `variations.CreateGlobal` asks for `mcp:write` on the project as a whole, and the typed constructor for MCP servers (`MCPServersInProject(projectID).Edit()`) arrives with the MCP slice. Rather than adding a second copy of that constructor in a sibling branch, or stacking this branch on the MCP slice, the variations service keeps an `engine` field for that one call and the inventory keeps its row, owned by P3 through a function override. _If reversed:_ merge the MCP slice's first branch here and switch the call; the row and the field then go.
2. **Registry discovery receives the client.** `mcpregistry.Service.AttachDiscovery` takes `*authz.Client` instead of the engine; `auth.Auth` is still built from the engine. _If reversed:_ none needed; discovery only ever made the one project-read check.
3. **Parity:** the externalmcp, resources, tools, variations, mcpregistry, templates and internalsessionissuers suites made 311 comparisons, all matching. `TestDiscoveryRealCredentialsHTTP/generated_dashboard_SDK_against_real_server` fails identically on the base branch here because it shells out to `mise`.

## P4 (AIS-846): dashboards, widgets and signals

### Decided without reviewer input

1. **Scope-parameterised helpers take a method expression.** `dashboards.authorize`, `widgets.authorize` and `sigint.authoringState` took an `authz.Scope`; they now take `func(authz.ProjectRef) authz.Operation`, and callers pass `authz.ProjectRef.Read` or `authz.ProjectRef.Write`. A caller can no longer pass a scope that is not a project permission. `sigint.requireAccess`, which already took a whole check, takes an `authz.Operation`. _If reversed:_ keep a scope argument and switch on it, as O1 did for product features.
2. **Analytics dataset checks stay on the engine.** Built-in dashboard visibility (`builtInReadable`) and `widgets.RequireDataset` evaluate the check each analytics dataset declares (`Dataset.AccessCheck`), which the logs slice owns. Both services keep an `engine` field for those calls only. _If reversed:_ give datasets a typed operation in the logs slice and drop the field.
3. **Owner-or-write stays a code branch.** `requireOwnerOrWrite` lets a dashboard's or widget's creator through before asking for `Project.Write()`. Modelling it as a fact-reading permission (`Is(owner)`) would be the first ownership fact in the project type and change challenge records for creators, so it stays as is. _If reversed (simplification):_ declare `edit_dashboard` as `Union(Rel(writer), Is(creator))` with a creator fact; the branch goes and the decision becomes explainable.
4. **Parity:** the dashboards, widgets, sigint and platformmcp suites made 850 comparisons, all matching.

## P5 (AIS-848): triggers, assistant memories and model keys

### Decided without reviewer input

1. **Same method-expression helpers as P4.** `triggers.requireAccess` and `assistantmemories.requireMemoryAccess` take `func(authz.ProjectRef) authz.Operation`; callers pass `authz.ProjectRef.Read` or `.Write`. Model keys use the operations directly.
2. **Assistant memories stay project-scoped.** Memories belong to assistants, which AS1 migrates, but today's check is `project:read`/`project:write` on the request's project; the slice keeps exactly that rather than introducing an assistant-memory permission. _If reversed:_ AS1 can move memories under the assistant type once assistants are declared.
3. **Parity:** the triggers, modelkeys and assistantmemories suites made 78 comparisons, all matching. The assistant-memories unit tests run without an organization and so exercise the engine path only, as production does for org-less contexts.

## E1 (AIS-852): environments

### Decided without reviewer input

1. **`environment` is its own type and its own unit.** It is declared under `project` with the project ID as a selector key, mirroring `mcp_server`: `reader` and `writer` grants on `environment:read`/`environment:write`, both blocks, and `write` excluded by either block, so the compiled scopes reproduce the frozen `environment:*` entries. It routes through a new `environment` unit that depends on `project`, so it can be shadowed and cut over separately from the rest of the project slice. Environments hold secrets, which is the reason to keep their rollout separate. _If reversed:_ fold the type into the `project` unit in `DefaultMigrationUnits()`; nothing else changes.
2. **`EnvironmentsInProject(projectID)` stands in for "no concrete environment".** Create, and the slug-miss paths of update, delete and clone, authorize against the project ID as the environment ID with the project dimension, exactly as `requireProjectEnvironmentWrite`/`Read` did: only a grant covering every environment of the project matches. It follows the MCP slice's `MCPServersInProject`. _If reversed:_ a separate `create_environment` permission on the project type would make the intent explicit, but stored `environment:write` grants with a project-wide selector would then need a projection.
3. **The environment declaration moved out of the evaluator test slice.** `registry_evaluator_test.go` declared an `environment` type for its own coverage; it now comes from the defaults, so the test declaration was removed to avoid a duplicate type.
4. **Parity:** the client corpus selects three more frozen fixtures (91 to 94), all passing, and the shadow corpus still matches everywhere. The environments suite made 87 comparisons (79 on environment operations), all matching.

## PL1 (AIS-855): plugins

### Decided without reviewer input

1. **No `plugin` type.** Plugin publication is already declared on `project` (`plugin_publisher`, `plugin_write_blocked`, `publish_plugin`) with the `plugin_publication` alternatives that fall back to organization administration, and every other plugins check is organization read or administration. No stored grant names an individual plugin, so a `plugin` type would have no grants of its own. _If reversed:_ declare `plugin` under `project` once per-plugin grants exist; `ProjectRef.PublishPlugin()` would move to it.
2. **`Engine.RequirePluginWrite` callers in plugins use `Project(org, id).PublishPlugin()`.** The engine method is `RequireAnyUnblocked` over the same two checks, and the client's engine form of the alternatives is exactly that call, so old mode is unchanged. The engine method is not an inventory entry point, so its 13 call sites never appeared in `consumers.csv`; this slice switches the nine in plugins. The skills calls belong to S1 and the Platform MCP call to X3. _If reversed:_ none; flagged so the inventory gap can be closed by adding `RequirePluginWrite` to `engineEntryPoints`.
3. **The plugins service keeps an `engine` field** for `admission.AdministratorWriteContext` (an admission context, not a decision) and for the skill-distribution `RequireAnyUnblocked` in `distribution_projection.go`, which S2 owns.
4. **The role-principal validator stays.** `assignments.normalizePrincipal` calls `authz.ValidatePrincipal`, which reads the role tables that stay authoritative until they are retired; its inventory row remains under PL1.
5. **Parity:** the plugins, access and cmd/gram suites made 1,864 comparisons and the Platform MCP suite (which builds the plugins service for delivery tests) 350, all matching. `TestProcessRoleDistributionSetup_DeleteWaitsForSetup` (a database lock-ordering test) fails identically on the base branch in this environment.

## M1 (AIS-859): toolsets and the MCP server constructor

### Decided without reviewer input

1. **One MCP server constructor, `MCPServer(projectID, serverID)`, with `Read`, `Edit` and `Connect`.** Every operation carries the project as the `project_id` dimension, always, so a grant or block bounded to one project can never match a server in another. Organization is not an argument: the stored MCP grants never name it and the active organization already bounds the policy. The governance package codes against the same shape. _If reversed:_ an organization argument can be added without changing any decision.
2. **Project-level MCP operations use `MCPServersInProject(projectID)`.** Creating, cloning into or listing servers is not about one existing server, and those grants have always been stored with the project ID as the resource. The reference keeps that resource ID, so every existing grant keeps its meaning. A cleaner model is a permission anchored at the project, as the RFC's `AnchoredAt("organization", "project", Self)` describes; that needs grant storage to change and belongs after X1. _If reversed:_ a project-anchored permission replaces the reference, and stored project-resource grants need a data migration.
3. **Behaviour change: project-level and server-level MCP checks now carry the project.** The capability inventory found 42 `mcp:read`/`mcp:write` checks that used the project ID as the resource with no `project_id` dimension. Permissive matching skips grant keys a check omits, so a grant bounded to project B (`{"resource_id":"*","project_id":"B"}`) passed in project A, and a block bounded to project A was ignored. The typed constructors close both holes. In toolsets this fixes `CloneToolset` (project-level write and the source server's read, both dimensionless) and `SetToolVariationsGroup` (a dimensionless server-level write); the other toolset checks already carried the project. New frozen fixtures pin the corrected behaviour: `allow_project_grant_rejects_project_level_check_for_other_project`, `allow_project_grant_matches_project_level_check_in_its_project` and `block_project_block_applies_to_project_level_check_in_its_project`. The existing fixtures about dimensionless checks still hold, because the matcher did not change; no consumer sends a dimensionless MCP check once the M tickets land. _If reversed:_ drop the dimension from `MCPServersInProject`; the cross-project allow returns.
4. **`ListToolsets` uses `MatchMany` in place of `Filter`.** Both answer each server independently with one challenge; `MatchMany` returns answers in input order, so the handler keeps toolsets by index instead of rebuilding an ID set.
5. **The toolsets service takes the client beside the engine.** The engine still builds `auth.Auth`, as in O1.

## AS1 (AIS-860): assistants and assistant identity

### Decided without reviewer input

1. **The skill type is declared here, not in S1.** `SendMessage` asks for skill read access, so this slice declares `skill` with its frozen scopes and adds a `skill` migration unit. S1 reuses the declaration and only adds its own operations. _If reversed:_ S1 declares the type and this slice depends on it; the assistant migration would wait for S1.
2. **Project-level skill and assistant checks keep targeting the project ID.** Today `SendMessage` checks `skill:read` with the project ID as the resource, and creating an assistant checks `assistant:write` with the project ID as both resource and `project_id`. `ProjectRef.ReadSkills()` and `AssistantsInProject(projectID).Write()` (shaped like M1's `MCPServersInProject`) reproduce those requests exactly, so grants match as before. _Worth simplifying later:_ a collection-level permission on the project (for example `project.create_assistant`) would say what the check means; it changes which grants match, so it needs its own approval.
3. **Assistant constructors follow M1's shape.** This branch is stacked on M1 and uses its `MCPServer(projectID, serverID)` for attachments. `Assistant(projectID, assistantID)` and `AssistantsInProject(projectID)` take no organization, for the same reason M1 gives: stored assistant grants never name it and the active organization bounds the policy.
4. **Grants a service loaded itself go through `Client.RequireLoaded` and `Client.EvaluateLoaded`.** `CheckActor` decides with the actor's own live grants, not the request's. The new calls take `LoadedPolicies` (organization, user and one or more grant sets, all of which must allow), keep the engine's refusal of principal credentials, and route like any other call. Loading stays in the service, as the RFC's responsibility table requires, so the `LoadGrants` and `ResolveUserPrincipals` rows stay in the inventory. _If reversed:_ the client would load grants itself, which moves transactions and locks into authz.
5. **A loaded decision outside an admitted request is its own boundary.** F6 pins routes at admission, so background work runs old. Assistant turns check their actor from the worker, where no request was admitted; a loaded call there reads its units' modes for the loaded organization and pins them for that call only. Inside an admitted request it reuses the request's pins. _If reversed:_ background loaded decisions stay on the engine until the worker gains admission boundaries.
6. **`EvaluateLoaded` honours blocks while the engine decides.** Its old path uses `GrantsAuthorize`, the effective decision with exclusions, rather than `GrantsSatisfy`, which ignores blocks. That is what the registry evaluator answers, so shadow compares like with like. No current caller relied on block-blind answers for a blockable scope.
7. **Agent selection moves to AG1.** `assistantidentity.(*Service).selectAgent` asks for `agent:authorize`, which the open owner-scoped agent PR (AIS-785) rewrites. The owner map now assigns it to AG1, which per the baseline waits for that PR.
8. **Parity is proven by the service's own tests in shadow.** The assistants, assistant identity and Platform MCP assistant identity suites run with `authztest.NewParityClient`.

## S1 (AIS-853): skills

### Decided without reviewer input

1. **New DSL option: `s.AcrossInstances()` on `Alternatives`.** Reading one skill accepts a project-wide `skill:read` grant (stored with the project ID as the resource) or a grant on that skill, and a block on either refuses. That is the same permission asked of two instances, which the compiler rejected as a repeated alternative. `AcrossInstances` lets an `Alternatives` declaration name one permission more than once; the typed constructor supplies the instance for each position. Without the option a repeat is still an error, and a new compile case pins that. The skill type declares `project_or_skill_read` with it and `VetoBlocks`. _If reversed:_ skill read becomes a project-anchored permission (`AnchoredAt("project", Self)`) once grant storage can express it; until then the engine call would stay in the service. _Worth simplifying later:_ the same pattern fits `MCPServersInProject` plus `MCPServer`, which M1 kept as two separate references.
2. **`Skill(projectID, skillID).Read()` and `ProjectRef.WriteSkills()`.** `Skill(...).Read()` is the alternatives above, and its engine form is exactly the old `RequireAnyUnblocked` over the project and skill checks, so old mode is unchanged. A new authz test holds it to the engine call across every case and mode. `WriteSkills` sits beside AS1's `ReadSkills` and targets the project ID, as the old checks did.
3. **This branch merges AS1 (and through it M1).** AS1 declares the `skill` type and the `skill` unit, and the MCP link checks need M1's `MCPServer` constructors, so S1 is stacked on PL1 with both merged. Conflicts on the shared append points were resolved by keeping every declaration; the corpus count is 98. _If reversed:_ S1 would wait for AS1 to land first and rebase.
4. **Behaviour change: skill link checks against project-level MCP now carry the project.** `ListMCPLinks` asked a dimensionless `mcp:read` on the project ID, the cross-project hole M1 describes. It now uses `MCPServersInProject(projectID).Read()`, so a grant bounded to another project no longer passes. Server-level link checks already carried the project and keep their meaning.
5. **Collection endpoints keep the project-wide skill grant.** A skill-access call with no skill ID used `RequireAnyUnblocked` over one check; it now uses `Require(ProjectRef.ReadSkills())`, which is the same decision.
6. **Distribution mutations use `Project.PublishPlugin()` or `Project.Write()`,** closing two more of the `RequirePluginWrite` call sites PL1 found outside the inventory.
7. **Remaining skills row:** `accessibleSkillIDs` resolves user principals for the distribution projection, which S2 owns; its owner override stays.
8. **Parity:** the skills and Platform MCP suites made 1,469 comparisons, all matching.

## S2 (AIS-854): skill and plugin projections

### Decided without reviewer input

1. **Distribution reads use `Skill(projectID, skillID).Read()`.** `distributionAuthContext` made the same project-or-skill `RequireAnyUnblocked` as the skills service, so it reuses S1's operation. The plugins service's `engine` field now only prepares administrator admission contexts.
2. **`plugins.RolePluginsForResource` takes `*authz.Client`.** Its organization administration and read checks become `Organization(id).Administer()` and `.Read()`, and its `RequirePluginWrite` fallback becomes `Evaluate(Project(org, id).PublishPlugin())`. As before, any failure to confirm publication leaves the matches undisclosed rather than failing the read.
3. **The access service takes the client beside the engine** for that one call. This touches the access service's constructor ahead of its own organization slice; the rest of access still uses the engine. _If reversed:_ the organization slice adds the parameter and S2 rebases onto it.
4. **The Platform MCP access reads keep their old answer.** They built a fresh engine for every call; they now build one client over a fresh engine at construction, with no migration router, so the client always answers through the engine. The Platform MCP slice will swap in the routed client when it threads the client through its config. _If reversed:_ thread the routed client into `NewAccessReadService` now.
5. **The principal and grant loaders stay in the services** (`ResolveDeliveryPrincipals`, `accessibleSkillIDs`, the `GrantsFromContext` presence check). They resolve who a projection is for, not whether the caller may act, and the RFC keeps loading in the service, as AS1 decision 4 describes. Their inventory rows remain under S2.
6. **Parity:** the plugins, access, Platform MCP and cmd/gram suites made 2,457 comparisons, all matching. `TestProcessRoleDistributionSetup_DeleteWaitsForSetup` fails on the base branch in this environment too.
