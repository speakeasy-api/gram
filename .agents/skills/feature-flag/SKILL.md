---
name: feature-flag
description: >
  Use when gating a feature behind a flag, dogfooding or gradually rolling out a
  change, choosing between `productfeatures` and PostHog feature flags, adding
  or checking a product feature or PostHog flag, removing a PostHog flag after
  rollout, or working out why a flag does not apply to an organization,
  project, or user.
metadata:
  relevant_files:
    - "server/internal/productfeatures/**/*.go"
    - "server/internal/productfeatures/queries.sql"
    - "server/design/shared/productfeatures.go"
    - "server/internal/feature/flags.go"
    - "server/internal/feature/provider.go"
    - "server/internal/thirdparty/posthog/posthog.go"
    - "client/dashboard/src/lib/featureFlags.ts"
    - "client/dashboard/src/hooks/useFeatureFlag.ts"
    - "client/dashboard/src/contexts/Telemetry.tsx"
---

## Two systems, two purposes

Speakeasy has two distinct feature-gating mechanisms. They are **not interchangeable** — pick based on the semantics of the flag, not on convenience.

|                     | `productfeatures`                                 | PostHog feature flags                      |
| ------------------- | ------------------------------------------------- | ------------------------------------------ |
| **Scope**           | Per-organization                                  | Per-organization, per-project, or per-user |
| **Who controls it** | Org admins (dashboard) or Speakeasy ops           | Engineering (PostHog console)              |
| **Persistence**     | PostgreSQL (`organization_features` table)        | PostHog platform                           |
| **Direction**       | Permanent capability toggle; maps to entitlements | Temporary rollout gate; removed once GA    |
| **Example use**     | "This org has SSO" / "This org was sold Risk"     | "Dogfood this UI change with our org"      |
| **Frontend**        | `useProductFeatures()` hook                       | `useFeatureFlag()` hook                    |

### Decision rule

> **Use `productfeatures`** when the toggle represents a durable, org-level capability that admins control or that maps to what the customer purchased (entitlement). Think: GCP's per-project API enablement.
>
> **Use PostHog flags** when you're rolling something out gradually, dogfooding a change internally, or need per-user granularity. The flag is expected to disappear once the feature ships to everyone.

---

## `productfeatures`

### Concepts

- Feature constants live in `server/internal/productfeatures/features.go` as typed string constants (`Feature` type).
- State is stored in `organization_features` (PostgreSQL). Soft-deletes track removal.
- `productfeatures.Client` checks Redis first (15 min TTL), falls back to Postgres, and returns errors on lookup failures — use `PlatformFeatureCheck` when you want silent false-on-error degradation.
- The management API (`/rpc/productFeatures.get` and `/rpc/productFeatures.set`) exposes the state to the dashboard and Speakeasy ops.

### Adding a new product feature

**1. Declare the constant** in [server/internal/productfeatures/features.go](../../../server/internal/productfeatures/features.go):

```go
const (
    FeatureMyNewFeature Feature = "my_new_feature"
)
```

**2. Add it to the Goa design** in [server/design/shared/productfeatures.go](../../../server/design/shared/productfeatures.go):

```go
// In the setProductFeature method's Enum constraint:
Enum("logs", "tool_io_logs", ..., "my_new_feature")

// In the getProductFeatures Result:
Attribute("my_new_feature_enabled", Boolean, "Whether my new feature is enabled")
```

**3. Wire the result** in [server/internal/productfeatures/snapshot.go](../../../server/internal/productfeatures/snapshot.go):

```go
MyNewFeatureEnabled: isEnabled(FeatureMyNewFeature),
```

**4. Regenerate** with `mise run gen:goa-server` (Goa codegen updates `gen/`).

### Checking a feature at runtime (Go)

```go
// inject *productfeatures.Client — it's already wired in cmd/gram/start.go
enabled, err := pf.IsFeatureEnabled(ctx, organizationID, productfeatures.FeatureMyNewFeature)
if err != nil {
    return fmt.Errorf("check my_new_feature: %w", err)
}
```

Use `PlatformFeatureCheck` (returns bool, logs errors, degrades to false) only in non-critical paths where you want silent degradation.

### Checking in the React dashboard

```tsx
const { data: features } = useProductFeatures();
if (features?.myNewFeatureEnabled) { ... }
```

---

## PostHog feature flags

### Concepts

- Flag keys are typed `Flag` constants in `server/internal/feature/flags.go`. The constant's comment is the one place per flag that records where it is evaluated (server, dashboard, or both) and how it is targeted.
- The server reaches PostHog only through `feature.Provider` (inject it; tests use `feature.InMemory`). The dashboard uses `useFeatureFlag()`.
- For org-scoped server gates, `distinctID` is the organization ID and `groups` is `feature.OrgProjectGroups(orgSlug, projectSlug)`: the `organization` group keyed by org slug and the `slug` group keyed by `<org>/<project>`, the same groups the dashboard registers.

### How the server evaluates a flag

In production the SDK caches flag definitions (polled every minute) and evaluates locally, calling PostHog only for conditions it cannot decide from the distinct ID and group keys the server passes; `IsFlagEnabledLocal` never calls out and adds person properties. Expect up to a minute for a flip to land, stale definitions during an outage, and an `Unable to compute flag locally (<key>)` debug line whenever a condition needs data the server does not send.

### Targeting a flag at people

Use `feature.PersonTarget(ctx)` for the distinct ID and person properties, and fall back to the organization ID when it returns `ok == false`; `mcpconnectionhealth.begin` is the example.

### Targeting rule for org and project flags

In the PostHog condition set, match by the `organization` group type and target "organization key is one of ..." (stored as `$group_key`); use the `slug` group key for project scope. Never target a group property such as `organization_slug`, a cohort, or person properties, except person properties a caller supplies through `IsFlagEnabledLocal` or `EvaluateFlag`: the server otherwise passes group keys only, so those conditions call PostHog on every evaluation and miss most orgs, in the dashboard too.

### Provider surface

| Call                                                                              | Returns             | Use when                                                                           |
| --------------------------------------------------------------------------------- | ------------------- | ---------------------------------------------------------------------------------- |
| `IsFlagEnabled(ctx, flag, distinctID, groups)`                                    | `bool, error`       | Plain rollout gate; cannot tell unavailable from off, so handle `err`              |
| `feature.EvaluateFlag(ctx, provider, flag, distinctID, groups, personProperties)` | `Evaluation, error` | Gates that fail closed; missing key, disabled provider, or error is Indeterminate  |
| `feature.FlagVariant(ctx, provider, flag, distinctID, groups)`                    | `Variant, error`    | Multivariate flags; map `""` or an error to the pre-rollout behaviour              |
| `FlagPayload(ctx, flag, distinctID, groups)`                                      | `[]byte, error`     | The flag carries config (e.g. a version pin); `nil` or an error means no clearance |
| `IsFlagEnabledLocal(ctx, flag, distinctID, groups, personProperties)`             | `bool, error`       | Hot paths that must never call PostHog; inconclusive reads as `false`              |

Fail closed: treat Indeterminate and errors as "unavailable", never as an explicit off, and never let a flag stand in for RBAC or an entitlement.

### Adding a new PostHog flag

**1. Declare the constant** in [server/internal/feature/flags.go](../../../server/internal/feature/flags.go) with a comment saying where it is evaluated and how it is targeted:

```go
const (
    // FlagMyNewFeature gates X. Evaluated server-side; targeted by PostHog
    // organization group (org slug). Fails closed. Removed once X is GA.
    FlagMyNewFeature Flag = "my-new-feature" // must match the key in PostHog
)
```

**2. Create the flag in PostHog before the gating code merges**; a missing key evaluates as Indeterminate and the gate stays closed. Write the description like `gram-budgets`: what it gates, where it is evaluated, how it is targeted, fail-closed behaviour.

**3. Verify the condition** with evaluation reasons (Feature flag → Evaluation reasons, or the `feature-flags-evaluation-reasons-retrieve` MCP tool) using `groups={"organization": "<slug>"}` for at least one target org and expect `condition_match`.

**4. Check at runtime** by injecting `feature.Provider`:

```go
groups := feature.OrgProjectGroups(authCtx.OrganizationSlug, "")
evaluation, err := feature.EvaluateFlag(ctx, s.features, feature.FlagMyNewFeature, authCtx.ActiveOrganizationID, groups, nil)
if err != nil {
    s.logger.WarnContext(ctx, "evaluate my-new-feature flag", attr.SlogError(err))
}
if evaluation != feature.EvaluationEnabled { // Disabled and Indeterminate both fail closed
    return oops.C(oops.CodeNotFound)
}
```

### Checking in the React dashboard

Use the typed `useFeatureFlag()` hook from `client/dashboard/src/hooks/useFeatureFlag.ts` and register keys in `FEATURE_FLAGS` (`client/dashboard/src/lib/featureFlags.ts`); never import PostHog hooks directly:

```tsx
const assistants = useFeatureFlag(FEATURE_FLAGS.assistants);

if (assistants.status === "loading") return null;
if (assistants.status === "missing" || assistants.status === "error") {
  return <FeatureUnavailable />;
}

const assistantsEnabled = assistants.status === "enabled";
```

The hook stays reactive as PostHog reloads flags. On localhost, the development telemetry provider reports every flag as enabled. PostHog flags control rollout UI only, never authorization or entitlement enforcement.

### Removing a flag after rollout

A flag is removed in code first and archived in PostHog only after that code is deployed. Read the live flag before you start, and decide which behavior to keep. Every row except the last needs the flag to be active, with no holdout, no flag dependency, and no earlier condition that serves a different value to some users:

| Flag state in PostHog                                                                                    | Keep                                   |
| -------------------------------------------------------------------------------------------------------- | -------------------------------------- |
| Boolean, one release condition at 100% with no properties                                                | The enabled path                       |
| Multivariate, one condition at 100% with no properties, whose override or 100% variant serves every user | That variant's path                    |
| Inactive, or every condition at 0%                                                                       | The off path (delete the feature code) |
| Anything else: partial rollout, targeted conditions, payload, kill switch, `*-shadow`                    | Nothing yet. A person decides first    |

Then:

1. Replace every read with the kept behavior: `feature.<Name>()` helpers, `EvaluateFlag`, `IsFlagEnabled`, `FlagVariant`, `useFeatureFlag(FEATURE_FLAGS.<key>)`, and string-literal reads of the key. Go to definition and find references from the `Flag` constant and from any helper in `server/internal/feature/` that wraps it, so no caller is missed.
2. Delete the branch you did not keep, the helper, the `Flag` constant, and the `FEATURE_FLAGS` key. Remove the key from the local defaults too: `server/flags.csv` and the development variant maps in `client/dashboard/src/contexts/Telemetry.tsx`.
3. Tests that assert the kept behavior must keep passing **unchanged**. Delete only tests that exercise the removed branch, such as a "flag off falls back to X" case. Tests start with every flag off, so a test that never mentions the flag may still cover only the removed branch. When a test fails after the removal, find which of these it is:
   - You kept the wrong branch or dropped a step the flag used to enable. Fix the code.
   - The test's setup only builds the data the off path reads (for example, rows the enabled path joins through are never created). Add that data to the setup.
   - The test asserts the off path's result. Delete it, like any other test of the removed branch.

   Never change an assertion about the kept behavior to make it pass.

4. Leave data-model cleanup to a later change. Columns, tables or backfills that only the removed branch used stay in place.
5. Do not archive or edit the PostHog flag in the same change. Archive it once the change is deployed and nothing still evaluates the key.

---

## Common mistakes

- **Don't use `productfeatures` for a temporary dogfood gate.** It creates API surface (`getProductFeatures` result field, Goa enum) that must be maintained indefinitely.
- **Don't use PostHog for entitlements.** PostHog is engineer-controlled and ephemeral; entitlement checks belong in `productfeatures` so org admins and billing can manage them.
- **Don't hardcode PostHog flag strings in multiple places.** Always declare them as constants in `feature/flags.go`.
- **Don't call the PostHog SDK directly from Go handlers.** Always go through the `feature.Provider` interface so tests can inject a noop.
