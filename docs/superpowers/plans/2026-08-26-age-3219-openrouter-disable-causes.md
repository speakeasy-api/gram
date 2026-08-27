# AGE-3219 OpenRouter Disable Causes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Preserve independent admin, trial, and billing reasons that disable a platform OpenRouter key, and expose those reasons only to platform administrators.

**Architecture:** Replace the mutable `disabled` column with a set-like `disable_causes text[]` and a stored generated `disabled` compatibility column. Put cause-aware, upstream-first transitions in the production OpenRouter provisioner, then make admin, trial, and billing callers add or remove only their own cause while they retain the existing per-key billing locks.

**Tech Stack:** PostgreSQL 16, Atlas migrations, SQLc, Go, Goa, React 19, TanStack Query, Vitest, generated TypeScript SDK.

**Spec:** [AGE-3219 approved Linear design](https://linear.app/speakeasy/issue/AGE-3219/re-arming-a-trial-silently-re-enables-a-key-a-platform-admin-locked)

## Global Constraints

- Causes are exactly `admin_lock`, `trial_demotion`, and `billing_inactive`.
- Treat `disable_causes` as a set. Never store duplicate values.
- Keep `disabled` as a stored generated value of whether `disable_causes` is non-empty.
- Backfill old `disabled = true` rows to `['admin_lock']`; backfill false rows to an empty array in the single Atlas migration.
- Each recovery path removes only its own cause. A key is enabled only when no causes remain.
- Preserve the existing per-key session lock, upstream-first order, and repeat-safe retries.
- Send an upstream disabled-state patch only when the effective enabled/disabled state changes. Limit patches remain allowed when policy requires a limit change.
- Admin lock changes write a key audit event even when another cause keeps the key disabled. Trial lifecycle changes use one organization event, not one event per key.
- Platform-admin API and key UI expose causes. Customer APIs do not expose causes.
- UI actions are **Disable key** and **Remove admin lock**. The UI cannot remove automatic causes.
- Local development provisioner methods remain no-ops.
- Do not edit files under `server/gen/`, `server/internal/**/repo/*.go`, or `client/dashboard/src/sdk/` by hand; regenerate them.
- Work and test locally before any PR-stack update. Never bypass review or safeguards.

---

### Task 1: Add the database cause set and cause mutation queries

**Files:**

- Modify: `server/database/schema.sql:2307-2335`
- Modify: `server/internal/thirdparty/openrouter/queries.sql:1-80`
- Create: the dated `server/migrations/*_openrouter-disable-causes.sql` file produced by Atlas
- Regenerate: `server/migrations/atlas.sum`
- Regenerate: `server/internal/thirdparty/openrouter/repo/models.go`
- Regenerate: `server/internal/thirdparty/openrouter/repo/queries.sql.go`
- Test: `server/internal/thirdparty/openrouter/disable_test.go`

**Interfaces:**

- Produces: `OpenrouterAPIKey.DisableCauses []string` while retaining read-only `OpenrouterAPIKey.Disabled bool`.
- Produces SQLc methods `AddOpenRouterAPIKeyDisableCause` and `RemoveOpenRouterAPIKeyDisableCause`, each scoped by organization ID and key type and returning the updated row.
- Preserves `UpdateOpenRouterKey` as a limit/hash update only; it must not clear disable causes.

- [ ] **Step 1: Add a failing repository-level test for set behavior**

Add table-driven cases to `disable_test.go` that seed a key, call the wished-for SQLc cause queries, and assert this sequence:

```go
added, err := queries.AddOpenRouterAPIKeyDisableCause(ctx, repo.AddOpenRouterAPIKeyDisableCauseParams{
    OrganizationID: orgID,
    KeyType:        string(KeyTypeChat),
    DisableCause:   string(DisableCauseAdminLock),
})
require.NoError(t, err)
require.Equal(t, []string{"admin_lock"}, added.DisableCauses)
require.True(t, added.Disabled)

addedAgain, err := queries.AddOpenRouterAPIKeyDisableCause(ctx, repo.AddOpenRouterAPIKeyDisableCauseParams{
    OrganizationID: orgID,
    KeyType:        string(KeyTypeChat),
    DisableCause:   string(DisableCauseAdminLock),
})
require.NoError(t, err)
require.Equal(t, []string{"admin_lock"}, addedAgain.DisableCauses)

withTrial, err := queries.AddOpenRouterAPIKeyDisableCause(ctx, repo.AddOpenRouterAPIKeyDisableCauseParams{
    OrganizationID: orgID,
    KeyType:        string(KeyTypeChat),
    DisableCause:   string(DisableCauseTrialDemotion),
})
require.NoError(t, err)
require.ElementsMatch(t, []string{"admin_lock", "trial_demotion"}, withTrial.DisableCauses)

withoutTrial, err := queries.RemoveOpenRouterAPIKeyDisableCause(ctx, repo.RemoveOpenRouterAPIKeyDisableCauseParams{
    OrganizationID: orgID,
    KeyType:        string(KeyTypeChat),
    DisableCause:   string(DisableCauseTrialDemotion),
})
require.NoError(t, err)
require.Equal(t, []string{"admin_lock"}, withoutTrial.DisableCauses)
require.True(t, withoutTrial.Disabled)
```

- [ ] **Step 2: Run the focused test and confirm RED**

Run:

```bash
mise run test:server ./internal/thirdparty/openrouter/ -run TestOpenRouterDisableCausesAreASet
```

Expected: compile failure because the cause constants, model field, and SQLc methods do not exist.

- [ ] **Step 3: Edit the desired schema**

Replace the writable Boolean with:

```sql
disable_causes TEXT[] NOT NULL DEFAULT '{}'::text[],
disabled BOOLEAN NOT NULL GENERATED ALWAYS AS (cardinality(disable_causes) > 0) stored,
```

Do not add an enum `CHECK`; allowed values are application validation.

- [ ] **Step 4: Add set-like SQL mutations and stop generic refresh from reinstating**

Use `@disable_cause = ANY(disable_causes)` to prevent duplicates and `array_remove` for removal:

```sql
-- name: AddOpenRouterAPIKeyDisableCause :one
UPDATE openrouter_api_keys
SET disable_causes = CASE
      WHEN @disable_cause::text = ANY(disable_causes) THEN disable_causes
      ELSE array_append(disable_causes, @disable_cause::text)
    END,
    updated_at = CASE
      WHEN @disable_cause::text = ANY(disable_causes) THEN updated_at
      ELSE GREATEST(clock_timestamp(), updated_at + INTERVAL '1 microsecond')
    END
WHERE organization_id = @organization_id
  AND key_type = @key_type
  AND deleted IS FALSE
RETURNING *;

-- name: RemoveOpenRouterAPIKeyDisableCause :one
UPDATE openrouter_api_keys
SET disable_causes = array_remove(disable_causes, @disable_cause::text),
    updated_at = CASE
      WHEN @disable_cause::text = ANY(disable_causes)
        THEN GREATEST(clock_timestamp(), updated_at + INTERVAL '1 microsecond')
      ELSE updated_at
    END
WHERE organization_id = @organization_id
  AND key_type = @key_type
  AND deleted IS FALSE
RETURNING *;
```

Remove `disabled = disabled AND NOT @reinstate` from `UpdateOpenRouterKey` and remove its `reinstate` parameter. Only explicit cause mutations can change disabled state.

- [ ] **Step 5: Generate and then repair the single Atlas migration**

Run:

```bash
mise run db:diff openrouter-disable-causes
```

Edit only the generated migration so the old Boolean remains available for the backfill. The migration body must have this order:

```sql
ALTER TABLE "openrouter_api_keys"
  ADD COLUMN "disable_causes" text[] NOT NULL DEFAULT '{}'::text[];

UPDATE "openrouter_api_keys"
SET "disable_causes" = ARRAY['admin_lock']::text[]
WHERE "disabled" IS TRUE;

ALTER TABLE "openrouter_api_keys"
  DROP COLUMN "disabled",
  ADD COLUMN "disabled" boolean NOT NULL
    GENERATED ALWAYS AS (cardinality(disable_causes) > 0) STORED;
```

Keep the exact Atlas quoting and header that `db:diff` generates. Do not create a second migration or a script.

- [ ] **Step 6: Rehash and regenerate SQLc**

Run:

```bash
mise run db:hash
mise run gen:sqlc-server
```

- [ ] **Step 7: Complete the cause constants needed by the test**

Create `server/internal/thirdparty/openrouter/disable_causes.go` with:

```go
package openrouter

type DisableCause string

const (
    DisableCauseAdminLock      DisableCause = "admin_lock"
    DisableCauseTrialDemotion  DisableCause = "trial_demotion"
    DisableCauseBillingInactive DisableCause = "billing_inactive"
)

func (c DisableCause) Validate() error {
    switch c {
    case DisableCauseAdminLock, DisableCauseTrialDemotion, DisableCauseBillingInactive:
        return nil
    default:
        return fmt.Errorf("unknown OpenRouter disable cause %q", c)
    }
}
```

Add the `fmt` import and a helper that converts `[]string` to validated `[]DisableCause` only if production code needs typed output.

- [ ] **Step 8: Run database and package checks**

Run:

```bash
mise run lint:migrations
mise run test:server ./internal/thirdparty/openrouter/ -run TestOpenRouterDisableCausesAreASet
```

Expected: migration lint passes and the set test passes.

- [ ] **Step 9: Commit the database slice**

```bash
git add server/database/schema.sql server/migrations server/internal/thirdparty/openrouter/queries.sql server/internal/thirdparty/openrouter/repo server/internal/thirdparty/openrouter/disable_causes.go server/internal/thirdparty/openrouter/disable_test.go
git commit -m "feat: store OpenRouter disable causes"
```

---

### Task 2: Make the production provisioner cause-aware

**Files:**

- Modify: `server/internal/thirdparty/openrouter/openrouter.go:310-340,540-735`
- Modify: `server/internal/thirdparty/openrouter/local.go:20-55`
- Modify: `server/internal/thirdparty/openrouter/disable_test.go`
- Modify: `server/internal/thirdparty/openrouter/unified_client_test.go`

**Interfaces:**

- Produces: `DisableCauseChange{CauseChanged bool, KeyAccessChanged bool}`.
- Produces provisioner methods:

```go
AddAPIKeyDisableCause(ctx context.Context, orgID string, keyType KeyType, cause DisableCause) (DisableCauseChange, error)
AddAPIKeyDisableCauseWithDB(ctx context.Context, db DBTX, orgID string, keyType KeyType, cause DisableCause) (DisableCauseChange, error)
RemoveAPIKeyDisableCause(ctx context.Context, orgID string, keyType KeyType, cause DisableCause, limit *int) (int, DisableCauseChange, error)
RemoveAPIKeyDisableCauseWithDB(ctx context.Context, db DBTX, orgID string, keyType KeyType, cause DisableCause, limit *int) (int, DisableCauseChange, error)
```

- `RefreshAPIKeyLimit` and `RefreshAPIKeyLimitWithDB` preserve every cause and never enable a key.
- Keep the legacy `DisableAPIKey*` and `ReinstateAPIKeyLimit*` methods temporarily in Task 2 so the repository compiles while callers migrate. Remove them only in Task 4.
- Add temporary no-op implementations of the new methods to every test mock that asserts `openrouter.Provisioner`; Task 4 replaces those stubs with cause-specific expectations.

- [ ] **Step 1: Write failing upstream-call matrix tests**

Add cases to `disable_test.go` that use the existing HTTP test server and assert:

```text
add admin_lock to enabled key             -> PATCH disabled=true once; CauseChanged=true; KeyAccessChanged=true
add trial_demotion to admin-locked key    -> no PATCH; CauseChanged=true; KeyAccessChanged=false
add existing cause                         -> no PATCH; CauseChanged=false; KeyAccessChanged=false
remove trial_demotion while admin remains -> no disabled PATCH; CauseChanged=true; KeyAccessChanged=false
remove final cause                          -> PATCH disabled=false once; CauseChanged=true; KeyAccessChanged=true
remove absent cause                         -> no disabled PATCH; CauseChanged=false; KeyAccessChanged=false
upstream failure                            -> local causes remain unchanged
retry after success                         -> no duplicate cause and no duplicate state PATCH
```

Also add a regression test that `RefreshAPIKeyLimit` on a disabled key updates the limit without sending `disabled=false` and without changing `disable_causes`.

- [ ] **Step 2: Run the focused tests and confirm RED**

Run:

```bash
mise run test:server ./internal/thirdparty/openrouter/ -run 'Test(Add|Remove)APIKeyDisableCause|TestRefreshAPIKeyLimit_PreservesDisableCauses'
```

Expected: compile failure because the new provisioner API and result type do not exist.

- [ ] **Step 3: Add the result type and provisioner interface methods**

Add to `openrouter.go`:

```go
type DisableCauseChange struct {
    CauseChanged    bool
    KeyAccessChanged bool
}
```

Validate both key type and cause at every public entry point. The `WithDB` forms must use the supplied locked connection for every local read and write.

- [ ] **Step 4: Implement upstream-first add behavior**

The internal add helper must:

1. Return a no-op for a missing key, matching current lifecycle behavior.
2. Read `DisableCauses` under the caller’s existing key lock.
3. Return a no-op when the cause already exists.
4. PATCH `{disabled:true}` only when the pre-change array is empty.
5. Add the cause locally only after the upstream PATCH succeeds.
6. Return `CauseChanged=true` and `KeyAccessChanged=(len(before)==0)`.

Do not send a limit in the disable PATCH.

- [ ] **Step 5: Implement upstream-first removal behavior**

The internal removal helper must:

1. Return a no-op when the cause is absent.
2. Resolve `limit` with the same explicit-limit and policy-default rules used today.
3. Set `Disabled` to `false` in the upstream request only when this removal empties the cause array.
4. Keep `Disabled=nil` when another cause remains.
5. Include a limit only when the caller supplied one or current policy repair requires it.
6. Remove the local cause only after the upstream request succeeds.
7. Return `KeyAccessChanged=true` only when the removed cause was the final cause.

If no disabled-state or limit change is needed, skip OpenRouter and perform only the local cause removal.

- [ ] **Step 6: Make generic limit refresh cause-neutral**

Delete the branch that uses a disabled row to set `patch.Disabled = new(false)`. A generic refresh can update `Limit` and `LimitReset`, but `Disabled` must remain nil. Replace every `UpdateOpenRouterKeyParams.Reinstate` use with the cause-neutral query shape from Task 1.

- [ ] **Step 7: Keep local development no-op behavior**

Implement all four new methods on `Development` without local or upstream writes:

```go
func (*Development) AddAPIKeyDisableCause(context.Context, string, KeyType, DisableCause) (DisableCauseChange, error) {
    return DisableCauseChange{}, nil
}

func (*Development) RemoveAPIKeyDisableCause(context.Context, string, KeyType, DisableCause, *int) (int, DisableCauseChange, error) {
    return 0, DisableCauseChange{}, nil
}
```

The `WithDB` variants delegate to the matching no-op.

- [ ] **Step 8: Keep the repository compiling during the API transition**

Find every test double with `var _ openrouter.Provisioner` or a constructor parameter of that type. Add temporary no-op implementations of the four new methods while keeping the old methods. Run a compile-only server pass:

```bash
mise run test:server ./... -run '^$'
```

Expected: every server package compiles without running tests.

- [ ] **Step 9: Run the core package suite**

```bash
mise run test:server ./internal/thirdparty/openrouter/
```

Expected: all OpenRouter tests pass, including upstream request-body assertions.

- [ ] **Step 10: Commit the provisioner slice**

```bash
git add server/internal/thirdparty/openrouter server/internal/admin server/internal/background server/internal/modelkeys server/internal/openrouterkeys server/internal/usage
git commit -m "feat: apply OpenRouter disable causes independently"
```

---

### Task 3: Expose causes and admin-lock semantics in the platform-admin API

**Files:**

- Modify: `server/design/platformadmin/openrouterkeys/design.go:15-145`
- Modify: `server/internal/openrouterkeys/queries.sql`
- Modify: `server/internal/openrouterkeys/impl.go:120-390`
- Modify: `server/internal/openrouterkeys/keys_test.go`
- Modify: `server/internal/openrouterkeys/setup_test.go`
- Modify: `server/internal/audit/openrouterkeys.go`
- Regenerate: `server/internal/openrouterkeys/repo/*`
- Regenerate: `server/gen/admin_open_router_keys/**` and `server/gen/http/admin_open_router_keys/**`

**Interfaces:**

- `AdminOpenRouterKey` adds required `disable_causes: array<string>` while retaining `disabled`.
- Existing `disableKey` adds `admin_lock`. Existing `enableKey` removes only `admin_lock`; its HTTP operation stays compatible, while descriptions and UI use “remove admin lock.”
- Key audit snapshots contain `disabled` and `disable_causes` before and after.

- [ ] **Step 1: Write failing admin service tests**

Extend `keys_test.go` with these cases:

```text
disable enabled key                 -> adds admin_lock, disables upstream, writes one audit row
disable trial-demoted key           -> adds admin_lock, skips upstream state patch, still writes audit row
disable key already admin-locked    -> repeat-safe, no new audit row
remove admin lock only cause         -> removes cause, enables upstream, writes one audit row
remove admin lock with trial cause   -> removes only admin_lock, skips enable patch, still writes audit row
remove when no admin lock            -> repeat-safe, automatic causes remain, no audit row
list/get response                     -> includes every active cause and generated disabled
```

Assert audit before/after snapshots show cause changes and contain no key material.

- [ ] **Step 2: Run admin tests and confirm RED**

```bash
mise run test:server ./internal/openrouterkeys/ -run 'Test(Disable|Enable|List)Key'
```

Expected: tests fail because the API model and handlers still use one Boolean.

- [ ] **Step 3: Add causes to the Goa design and admin SQL**

In `AdminKey`, add a required array field with element enum values:

```go
Attribute("disable_causes", ArrayOf(String, func() {
    Enum("admin_lock", "trial_demotion", "billing_inactive")
}), "Independent reasons that keep the key disabled.")
```

Change the `enableKey` description to say that it removes only the platform-admin lock. Select `k.disable_causes` in both admin queries.

- [ ] **Step 4: Regenerate SQLc and Goa server files**

```bash
mise run gen:sqlc-server
mise run gen:goa-server
```

Do not hand-edit generated files.

- [ ] **Step 5: Implement admin cause transitions**

Map `DisableCauses` into every `AdminOpenRouterKey` response. In both handlers, retain `keybillinglock.WithAcquireTimeout`.

- `DisableKey` calls `AddAPIKeyDisableCause(..., DisableCauseAdminLock)`.
- `EnableKey` calls `RemoveAPIKeyDisableCause(..., DisableCauseAdminLock, recordedLimit)`.
- Write an audit event when `CauseChanged` is true, regardless of `KeyAccessChanged`.
- Do not write an audit event on an idempotent retry.
- Never remove `trial_demotion` or `billing_inactive` in this service.

Use the locked `*pgxpool.Conn` through the provisioner `WithDB` methods; do not reacquire from the pool inside the callback.

- [ ] **Step 6: Add cause snapshots to admin key audit entries**

Use one snapshot type for lock add/remove:

```go
type OpenRouterAPIKeyDisableSnapshot struct {
    Disabled      bool     `json:"disabled"`
    DisableCauses []string `json:"disable_causes"`
}
```

Pass explicit before and after snapshots from the handler. Keep action names compatible unless an existing consumer requires a new name; the UI copy, not the audit action, says **Remove admin lock**.

- [ ] **Step 7: Run the admin service suite**

```bash
mise run test:server ./internal/openrouterkeys/
```

Expected: all service, locking, authorization, and audit tests pass.

- [ ] **Step 8: Commit the admin API slice**

```bash
git add server/design/platformadmin/openrouterkeys server/internal/openrouterkeys server/internal/audit/openrouterkeys.go server/gen/admin_open_router_keys server/gen/http/admin_open_router_keys
git commit -m "feat: manage OpenRouter admin locks by cause"
```

---

### Task 4: Update trial and billing lifecycle callers

**Files:**

- Modify: `server/internal/background/activities/trial_demotion.go`
- Modify: `server/internal/background/activities/trial_demotion_test.go`
- Modify: `server/internal/admin/impl.go:89-140,1260-1345`
- Modify: `server/internal/admin/rearmtrial_test.go`
- Modify: `server/internal/background/activities/reconcile_payg_openrouter_chat_key.go`
- Modify: `server/internal/background/activities/reconcile_payg_openrouter_chat_key_test.go`
- Modify: `server/internal/background/activities/queries.sql` if the PAYG projection needs trial conversion state
- Regenerate: `server/internal/background/activities/repo/*` when its SQL changes
- Modify: mocks that implement `openrouter.Provisioner`, including `server/internal/thirdparty/openrouter/unified_client_test.go` and relevant usage integration tests

**Interfaces:**

- Trial demotion adds `trial_demotion` to both key types.
- Trial re-arm removes `trial_demotion` from both key types and returns whether any effective access changed for later AGE-3150 audit work.
- Billing loss adds `billing_inactive` to the chat key only. Billing recovery removes only `billing_inactive`.
- A Stripe checkout that has converted a previously demoted trial also removes `trial_demotion` from both keys based on authoritative trial state; ordinary billing recovery does not.

- [ ] **Step 1: Write failing trial tests for independent causes**

In `trial_demotion_test.go`, seed an admin-locked key and verify demotion appends `trial_demotion` without an upstream disabled patch. In `rearmtrial_test.go`, cover:

```text
{trial_demotion}                  -> re-arm removes it and enables the key
{admin_lock, trial_demotion}      -> re-arm removes only trial_demotion and key stays disabled
{billing_inactive, trial_demotion}-> re-arm removes only trial_demotion and key stays disabled
missing key                        -> remains a no-op
repeated re-arm                    -> no repeated state patch
```

Keep the existing guarantee that required key work happens before organization restoration commits.

- [ ] **Step 2: Write failing billing tests for independent causes**

In `reconcile_payg_openrouter_chat_key_test.go`, cover:

```text
billing loss on enabled chat key                 -> add billing_inactive and disable upstream
billing loss on admin-locked chat key             -> add billing_inactive, no upstream state patch
billing recovery with billing_inactive only       -> remove it and enable upstream
billing recovery with admin_lock also present     -> remove billing_inactive only, remain disabled
ordinary billing recovery                          -> never removes trial_demotion
Stripe conversion of demoted trial                -> remove trial_demotion from chat and internal
retries                                             -> preserve set semantics and avoid repeated state patches
```

- [ ] **Step 3: Run lifecycle tests and confirm RED**

```bash
mise run test:server ./internal/background/activities/ ./internal/admin/ -run 'Test(DemoteExpiredTrials|RearmTrial|ReconcilePaygOpenRouter)'
```

Expected: compile or assertion failures because lifecycle callers still use global disable/reinstate methods.

- [ ] **Step 4: Change trial demotion to add its cause**

Update the locked call for each key type to:

```go
change, err := dbProvisioner.AddAPIKeyDisableCauseWithDB(
    ctx, conn, args.OrganizationID, keyType, openrouter.DisableCauseTrialDemotion,
)
```

Keep missing keys as no-ops. Aggregate `change.KeyAccessChanged` at organization scope for the later audit contract, but do not write per-key trial events.

- [ ] **Step 5: Change re-arm to remove only trial demotion**

Replace “revive every disabled key” checks with “remove `trial_demotion` when present.” Pass the existing recorded ceiling before commit and preserve the post-commit recap for legacy zero ceilings. Do not skip a row merely because generated `Disabled` is false; inspect causes. An admin or billing cause must survive the transaction.

Update `TrialKeyReviver` and `TrialKeysUnavailable` to the new cause-aware interface.

- [ ] **Step 6: Change PAYG reconciliation to own only billing cause**

- Base tier without subscription: add `billing_inactive` to chat.
- PAYG with subscription: remove `billing_inactive` from chat and apply PAYG limit policy.
- Delete the old binary-disabled shortcut that re-enabled Security inference merely because checkout occurred.
- If the authoritative trial row shows that Stripe converted a demoted trial, remove `trial_demotion` from both chat and internal keys as a separate conversion reconciliation step. This is the trial-conversion owner; it must not be conflated with removing `billing_inactive`.
- Keep both key-type billing locks and preserve newer explicit spend-cap choices.

- [ ] **Step 7: Update all mocks and compile-time interface assertions**

Replace mock expectations on `DisableAPIKey` and `ReinstateAPIKeyLimit` with the cause-aware methods and explicit causes. Keep `RefreshAPIKeyLimit` mocks only for cause-neutral limit changes.

Remove the legacy `DisableAPIKey*` and `ReinstateAPIKeyLimit*` methods from `Provisioner`, `OpenRouter`, `Development`, and all test doubles only after every production caller uses the cause-aware methods.

- [ ] **Step 8: Run focused and package suites**

```bash
mise run test:server ./internal/background/activities/ ./internal/admin/ ./internal/usage/
```

Expected: lifecycle, locking, retry, Stripe integration, and audit tests pass.

- [ ] **Step 9: Commit the lifecycle slice**

```bash
git add server/internal/background server/internal/admin server/internal/usage server/internal/thirdparty/openrouter/unified_client_test.go
git commit -m "fix: preserve OpenRouter causes across trial and billing changes"
```

---

### Task 5: Show causes and admin-lock actions in the platform-admin key UI

**Files:**

- Regenerate: `client/dashboard/src/sdk/**`
- Modify: `client/dashboard/src/pages/platform-admin/OpenRouterKeys.tsx`
- Create: `client/dashboard/src/pages/platform-admin/openRouterKeyState.ts`
- Create: `client/dashboard/src/pages/platform-admin/openRouterKeyState.test.ts`
- Create or modify: `client/dashboard/src/pages/platform-admin/OpenRouterKeys.test.tsx` if the existing dashboard harness supports a focused component test without excessive fixtures

**Interfaces:**

- `AdminOpenRouterKey.disableCauses: string[]` comes from the generated SDK.
- `openRouterKeyState.ts` exports cause labels and pure action eligibility helpers so combined causes are covered without coupling every test to the table harness.

- [ ] **Step 1: Write failing UI state tests**

Create pure helper tests for:

```ts
expect(causeLabels(["admin_lock", "trial_demotion"])).toEqual([
  "Admin lock",
  "Trial demotion",
]);
expect(keyAction([])).toBe("disable");
expect(keyAction(["trial_demotion"])).toBe("disable");
expect(keyAction(["admin_lock"])).toBe("remove-admin-lock");
expect(keyAction(["admin_lock", "billing_inactive"])).toBe("remove-admin-lock");
```

Add a component assertion that automatic causes have no remove action and that combined causes are visible.

- [ ] **Step 2: Run the focused UI tests and confirm RED**

```bash
aube run -F dashboard test -- src/pages/platform-admin/openRouterKeyState.test.ts src/pages/platform-admin/OpenRouterKeys.test.tsx
```

Expected: failure because the helper, generated field, and revised labels do not exist.

- [ ] **Step 3: Regenerate the TypeScript SDK**

```bash
mise run gen:sdk
```

If this command requires `SPEAKEASY_API_KEY`, stop and report the credential blocker. Do not hand-edit generated SDK files.

- [ ] **Step 4: Implement cause labels and action eligibility**

Use this fixed label map:

```ts
export const DISABLE_CAUSE_LABELS = {
  admin_lock: "Admin lock",
  trial_demotion: "Trial demotion",
  billing_inactive: "Billing inactive",
} as const;
```

Unknown future values must render as their raw value instead of disappearing. Return `remove-admin-lock` only when `admin_lock` is present; otherwise return `disable`.

- [ ] **Step 5: Update the key table**

- Keep the generated `disabled` field for usage polling and the Enabled/Disabled badge.
- Add active causes under the status or in a dedicated Causes column; all active causes must be readable without opening raw JSON.
- Label the add action **Disable key**.
- Label the remove action **Remove admin lock**.
- When only automatic causes exist, offer **Disable key** so the admin can add an independent lock.
- Never offer controls that remove `trial_demotion` or `billing_inactive`.
- Keep the existing mutation invalidation and Sonner feedback, but make success copy say “Admin lock added” or “Admin lock removed.”

- [ ] **Step 6: Run UI tests and type checking**

```bash
aube run -F dashboard test -- src/pages/platform-admin/openRouterKeyState.test.ts src/pages/platform-admin/OpenRouterKeys.test.tsx
aube run -F dashboard type-check
```

Expected: focused tests and TypeScript compilation pass.

- [ ] **Step 7: Commit the UI and generated SDK slice**

```bash
git add client/dashboard/src/sdk client/dashboard/src/pages/platform-admin
git commit -m "feat: show OpenRouter disable causes to admins"
```

---

### Task 6: Verify migration, server, UI, and scope

**Files:**

- Review only: all files changed by Tasks 1-5

**Interfaces:**

- Consumes the complete AGE-3219 implementation.
- Produces local verification evidence for user approval before any PR update.

- [ ] **Step 1: Verify generated artifacts and formatting**

```bash
hk fix
git diff --check
mise run db:hash
mise run lint:migrations
```

Confirm `git diff server/migrations/atlas.sum` contains only the new migration hash and that no generated file was hand-edited.

- [ ] **Step 2: Run focused server tests**

```bash
mise run test:server ./internal/thirdparty/openrouter/ ./internal/openrouterkeys/ ./internal/background/activities/ ./internal/admin/ ./internal/usage/
```

Expected: all focused server packages pass.

- [ ] **Step 3: Run server lint on the changed backend**

```bash
mise lint:server
```

Expected: no lint errors.

- [ ] **Step 4: Run dashboard checks**

```bash
aube run -F dashboard test -- src/pages/platform-admin/openRouterKeyState.test.ts src/pages/platform-admin/OpenRouterKeys.test.tsx
aube run -F dashboard type-check
```

Expected: tests and type check pass.

- [ ] **Step 5: Inspect migration and public-surface privacy**

Verify manually from the diff:

```text
true rows backfill to exactly {admin_lock}
false rows backfill to {}
disabled is generated from cardinality(disable_causes) > 0
no customer API or customer SDK contains disable_causes
platform-admin API and dashboard SDK contain disable_causes
no automatic lifecycle path removes admin_lock
no admin path removes trial_demotion or billing_inactive
```

- [ ] **Step 6: Review branch scope**

```bash
git status --short
git diff --stat origin/main...HEAD
git log --oneline --decorate origin/main..HEAD
```

Confirm the original worktree’s untracked files are absent and untouched. Do not push or update a PR yet.

- [ ] **Step 7: Present local evidence to the user**

Report the migration shape, focused test counts, lint/type-check results, and any `gen:sdk` credential blocker. Ask for approval before updating the PR stack.
