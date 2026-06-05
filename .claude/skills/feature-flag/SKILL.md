---
name: feature-flag
description: >
  Decision guide and implementation patterns for Gram's two feature-gating systems:
  `productfeatures` (org-level, admin-configurable, entitlement-tied) vs PostHog feature
  flags (per-user/per-group rollout, engineering-controlled). Activate whenever the task
  involves gating a feature behind a flag, dogfooding a change, deciding which system to
  use, or adding a new product feature or PostHog flag.
metadata:
  relevant_files:
    - "server/internal/productfeatures/**/*.go"
    - "server/internal/productfeatures/queries.sql"
    - "server/design/productfeatures/design.go"
    - "server/internal/feature/flags.go"
    - "server/internal/feature/provider.go"
    - "server/internal/thirdparty/posthog/posthog.go"
    - "client/dashboard/src/pages/**"
---

## Two systems, two purposes

Gram has two distinct feature-gating mechanisms. They are **not interchangeable** — pick based on the semantics of the flag, not on convenience.

|                     | `productfeatures`                                 | PostHog feature flags                   |
| ------------------- | ------------------------------------------------- | --------------------------------------- |
| **Scope**           | Per-organization                                  | Per-user or per-group                   |
| **Who controls it** | Org admins (dashboard) or Speakeasy ops           | Engineering (PostHog console)           |
| **Persistence**     | PostgreSQL (`organization_features` table)        | PostHog platform                        |
| **Direction**       | Permanent capability toggle; maps to entitlements | Temporary rollout gate; removed once GA |
| **Example use**     | "This org has SSO" / "This org was sold Risk"     | "Dogfood this UI change with our org"   |
| **Frontend**        | `useProductFeatures()` hook                       | `posthog.isFeatureEnabled()`            |

### Decision rule

> **Use `productfeatures`** when the toggle represents a durable, org-level capability that admins control or that maps to what the customer purchased (entitlement). Think: GCP's per-project API enablement.
>
> **Use PostHog flags** when you're rolling something out gradually, dogfooding a change internally, or need per-user granularity. The flag is expected to disappear once the feature ships to everyone.

If you're gating a dev-phase UI change that you only want your dogfooding org to see → **PostHog**.
If you're shipping a capability that some customers pay for and others don't → **`productfeatures`**.

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

**2. Add it to the Goa design** in [server/design/productfeatures/design.go](../../../server/design/productfeatures/design.go):

```go
// In the setProductFeature method's Enum constraint:
Enum("logs", "tool_io_logs", ..., "my_new_feature")

// In the getProductFeatures Result:
Attribute("my_new_feature_enabled", Boolean, "Whether my new feature is enabled")
```

**3. Wire the result** in [server/internal/productfeatures/impl.go](../../../server/internal/productfeatures/impl.go):

```go
MyNewFeatureEnabled: isEnabled(FeatureMyNewFeature),
```

**4. Regenerate** with `mise generate` (Goa codegen updates `gen/`).

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

- Flag constants live in `server/internal/feature/flags.go` as a typed `Flag` string.
- The `feature.Provider` interface (`IsFlagEnabled(ctx, flag, distinctID)`) is the only server-side API — inject it, don't call PostHog directly.
- The PostHog implementation polls the PostHog platform; a noop stub is used in tests.
- `distinctID` is typically the user's ID or email. For org-level targeting you can use the org ID and configure matching groups in PostHog.

### Adding a new PostHog flag

**1. Declare the constant** in [server/internal/feature/flags.go](../../../server/internal/feature/flags.go):

```go
const (
    FlagMyNewFeature Flag = "my-new-feature"  // must match the key in PostHog
)
```

**2. Create the flag in PostHog** — set release conditions (e.g., "users in group X") via the PostHog console. The string key must match exactly.

**3. Check at runtime** by injecting `feature.Provider`:

```go
enabled, err := featureProvider.IsFlagEnabled(ctx, feature.FlagMyNewFeature, userDistinctID)
if err != nil || !enabled {
    // flag off or unavailable
}
```

### Checking in the React dashboard

Use the PostHog JS SDK directly (already initialized in the dashboard):

```tsx
import { useFeatureFlagEnabled } from "posthog-js/react";

const myFeatureEnabled = useFeatureFlagEnabled("my-new-feature");
```

---

## Common mistakes

- **Don't use `productfeatures` for a temporary dogfood gate.** It creates API surface (`getProductFeatures` result field, Goa enum) that must be maintained indefinitely.
- **Don't use PostHog for entitlements.** PostHog is engineer-controlled and ephemeral; entitlement checks belong in `productfeatures` so org admins and billing can manage them.
- **Don't hardcode PostHog flag strings in multiple places.** Always declare them as constants in `feature/flags.go`.
- **Don't call the PostHog SDK directly from Go handlers.** Always go through the `feature.Provider` interface so tests can inject a noop.
