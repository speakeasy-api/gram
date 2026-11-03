# RBAC foundation decision log

This log records every design decision taken while implementing the Backend Revamp milestone (parent AIS-830) against the RFC "Practical RBAC: authorization foundation". Each entry names the ticket, the choice, the reasoning and who made it.

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

1. **No grant-only flag on machine policies.** The RFC says machine layers are grant-only. The evaluator keeps the actor fixed across every policy and `Is` only matches a user actor, so an agent or workload cannot use an owner's authorship in any policy. That gives the same guarantee without a flag a caller could forget to set. `TestMachineActorNeverUsesOwnerAuthorship` pins it. *If reversed:* add a per-policy flag that disables `Is` and `Via`; no caller changes otherwise.
2. **Captured boundaries carry only policies.** A captured boundary adds its policies to the intersection. Re-checking a captured relationship is done by the caller evaluating again with the captured facts, rather than the request carrying a second fact map. *If reversed:* add captured facts to `PolicySet` and evaluate captured policies against them.
3. **Deny plus undecidable resolves to deny.** If one policy denies and another cannot be decided, the result is a denial rather than an error. It fails closed and makes the outcome independent of policy order. *If reversed:* `decide` returns the first error even when another policy denies; callers would see `unexpected` instead of `forbidden` in that case.
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

1. **The default registry grows one slice at a time.** `DefaultRegistry()` starts with the organization, project, MCP server and MCP auth-client types, whose compiled scopes already reproduce the frozen scope tables; those declarations moved from tests into `declarations.go`. Each migration ticket adds its own types. *If reversed:* declaring every type up front would need every scope's semantics settled before any service moves, which the per-slice tickets exist to do.
2. **The client wraps the engine.** Admission, grant loading, the enforcement profile, challenge logging and error mapping are the engine's; only the decision moves to the registry evaluator. *If reversed:* the client would duplicate admission, which the ticket says to reuse.
3. **No ad-hoc `RequireAny` on the client.** The client answers named alternatives the registry declares, as the RFC describes. A service that today calls `RequireAny` declares an `Alternatives` set in its migration. *If reversed:* add `Client.RequireAny(ops...)`; callers could then combine arbitrary permissions again.
4. **Callers without an authenticated actor act as `system:unattributed`.** Assistant tokens, embedded chat sessions and similar callers have no admitted actor. Their attribution never becomes a user, so ownership facts never match them, matching the RFC's rule that attribution never selects a human caller profile. *If reversed:* those callers would match `Is(owner)` facts for their minting user.
5. **An admission with no policies denies.** The engine denies a prepared context with no policies; the client keeps that instead of returning `ErrNoPolicies`. A missing admission is still an error.
6. **Startup compiles the client; services receive it as they migrate.** `start.go` builds the default client and stops startup if the registry fails to compile. No service holds the client yet, so there is no unused injection; the first migration ticket passes it to its service.
7. **Typed constructors are not inventoried.** A consumer that calls `authz.Project(...).Read()` has migrated, so it drops out of `consumers.csv`. The inventory shrinking to wiring rows is the migration's progress signal.
8. **Typed operations take their resource kind from the declared type.** The kind-override fixture is skipped with that reason, as F3 already approved for the evaluator.
