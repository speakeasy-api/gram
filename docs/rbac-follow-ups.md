# RBAC migration follow-ups

Tracked in Linear as AIS-880 (rename: AIS-881). Follow-up work that the migration PRs deliberately left for later, collected in one place. Each item names where it was parked and what it waits on. Nothing here changes behaviour on its own until its PR lands.

## Ready to run

1. **Rename facts to attributes (AIS-881).** Approved by Tiago on 2026-11-04. `Fact` becomes `Attribute`, along with its constructors (`StringAttribute`, `BoolAttribute`, `RelatedAttribute`, `AbsentAttribute`, ...), `FactState` → `AttributeState`, `WithFacts` → `WithAttributes`, `RequiredFacts` → `RequiredAttributes`, `ErrFactUnresolved` → `ErrAttributeUnresolved`, and the schema accessors `Cond.Fact()` and `Projection.Fact()`. That is about 350 identifier uses in `server/internal/authz`, about 13 consumer files (mostly chat and agents) and 4 docs (decision log, cutover routing, migrating-a-service guide, `rbac.md`). Unrelated look-alikes stay: `Factory`, `SupportFact`, `RoutingFacts`. One mechanical PR on the integration branch, with no behaviour change. Not started: it runs when Tiago asks.
2. **Unify the client field name.** Plugins and the projects stack call the client field `authz`, and access calls it `authzClient` (integration decision 2 on #8519). Pick one name and rename it in a single pass. Low priority, cosmetic.
3. **Stop leaking the engine type into the client.** `NewClient` takes `*Engine`, but the client only uses it for two different jobs. The first is request admission: `PrepareContext`, `RefreshContext`, `ShouldEnforce`, the admission boundary, grant loading from the stored tables, plus `mapError` and the logger. That job outlives the engine and should move into the client, or into an admission component the client owns. The second is the old-mode decider (`Require`, `RequireAny`, `RequireAnyUnblocked`, `Evaluate`, `EvaluateLoadedGrants`, `FindMatched`, `RequireUserOrganizationScopeWithDBTX`), which the client should take as a narrow unexported interface. After this, removing the engine means deleting one adapter and the old and shadow modes, and no call site changes. One PR on the integration branch, with no behaviour change.
4. **Drop the engine from services that no longer call it.** About 100 non-test files still reference `*authz.Engine`. Services whose last engine call has moved to the client can lose the parameter now, without waiting for shadow data. Check each one first: some of the sign-in services in #8531 still use the engine elsewhere, for example `agentmanagement` and `keys` build `agentmanagement.NewAuthorizer(authzEngine)` until the agents stack lands. Services that still call the engine directly (stored-grant writers, explanation helpers, Platform MCP rows) keep it until X1 or X3.

## Waiting on a decision

5. **Chat list cutover onto the Q1 filter.** #8534 adds `Plan`/`AccessPlan`, but chat collections still list through the old queries. The cutover waits on the five list-versus-single-chat divergences characterized in #8533: a project-wide `chat:read` grant, a single-chat grant, legacy API keys, dashboard chat-session tokens, and a failing visibility check.
6. **Member agent self-creation.** #7590 removed it, and #8524 flags it at the top. The recommended fix is a default Member grant `agent:write {owner: self}`. The alternative is restoring self-creation in #7590.
7. **Confirm `EvaluateGrants` keeps no transport refusal.** F6 decision 21 deliberately keeps `GrantsAuthorize`'s behaviour: the grants belong to another principal, so the request's own credential does not decide the answer, and adding the refusal would turn allows into denials. Only the loaded-policy entry points reject principal credentials. The review threads on #8465, #8487 and #8531 ask Tiago to confirm this. No change is planned unless he reverses it.
8. **Expired grants.** `principal_grants` has no `expires_at`, so #8481 cannot honour expiry yet.
9. **Two security threads on #8519.** The risk scanner runs with a nil authorization (no repro yet), and Platform MCP `connection_mutation` uses project-level `mcp:write`.

## Waiting on another PR

10. **Chat ownership permissions.** Once AG1 (#8524) declares `agent.is_owner`, declare the attribute-reading `chat.read` and `chat.write` from the RFC, load the chat's attributes in `loadAuthorizedChat`, and point `Chat(...).Read()` and `.Write()` at them (chat decision 1). No call site changes.
11. **#7590 lands first.** The agents stack (#8524, #8530, #8532) is based on it. It needs an SDK lockfile regeneration and the Agents page edits listed in #8524.

## Waiting on production shadow data (release gate)

The RFC lets the engine and scope tables go only after production shadow comparisons are classified and resolved. These items start after rollout.

12. **X1, catalogue, validation and seeding (AIS-873).** Derive these from the compiled registry. X1 also waits on the agent and workload declarations (AG1, AG2, W1).
13. **X2, runtime and key policy projections (AIS-874).**
14. **X4, demo seed and dashboard gates (AIS-876).** This includes the "blocked" label for shared sign-in that was deferred from the MCP stack.
15. **Z-stage cleanup (Z1, Z2).** Remove what the migration kept alive on purpose:
    - the explanation helpers left in `explain_access.go` (13 rows, 15 call sites), plus the tool-discovery explanation;
    - access grant loaders and writers, and grant loading through `LoadGrants` for plugin servers, device-agent connections and role snapshots (M6 decision 6);
    - the engine itself: the old-mode decider adapter from item 3, the old and shadow routing modes, the shadow recorder and the migration flags;
    - the missing-client fallbacks for workers and commands (O3, O5, O6 activities and billing);
    - the auth-client backfill planner (`authclientbackfill`), once X1 retires the tables it writes;
    - quiet-engine shims in the client (old-mode `Evaluate` challenge recording, `ShouldEnforce` forwarding);
    - `docs/rbac.md` and the `gram-rbac` skill, rewritten for the registry.

## Keeping this current

When a follow-up lands, link its PR here and in Linear and strike the item. When a PR parks something new for later, add it here in the same change.
