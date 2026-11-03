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
