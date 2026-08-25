# AGE-3342 Admin Audit Surface Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Record admin-app audit events with `acting_surface = admin`, keep the real staff name for the private admin Activity API, and mask that name as `Speakeasy Team` in customer audit responses.

**Architecture:** Extend the existing closed audit-surface set. Derive the new surface from `AdminAuthContext`. Store the staff name in the existing `actor_display_name` field. Apply the customer mask when an event has the `admin` surface, without removing the existing Speakeasy-organization membership mask. Propagate the surface into the durable admin spend-cap activity, where the request context does not survive. No database migration is necessary because `audit_logs.acting_surface` is already a text field.

**Tech Stack:** Go, PostgreSQL/sqlc, Temporal activities, Goa service internals, Testify

**Required skills:** `test-driven-development`, `postgresql`, `verification-before-completion`

---

### Task 1: Derive the `admin` acting surface

**Files:**

- Modify: `server/internal/audit/surface_internal_test.go`
- Modify: `server/internal/audit/surface.go`

- [ ] **Step 1: Add failing surface tests**

Add an admin case to `TestActingIdentityFromContext_Derivation`. Build the context with a non-empty `AdminAuthContext.SessionID`, `OIDCSubject`, `Name`, and `Email`. Require `SurfaceAdmin`.

Update `TestKnownSurfaces_AreLowCardinality` to require six values and to contain `SurfaceAdmin`. Keep the unrecognized-mark test unchanged.

- [ ] **Step 2: Run the focused tests and confirm the expected failure**

Run:

```bash
mise run test:server ./internal/audit/ -run '^(TestActingIdentityFromContext_Derivation|TestKnownSurfaces_AreLowCardinality)$' -count=1
```

Expected: failure because `SurfaceAdmin` does not exist and admin auth still resolves to `unknown`.

- [ ] **Step 3: Add the surface and derivation rule**

In `surface.go`:

- Add `SurfaceAdmin Surface = "admin"` with a comment that it is the isolated Google-authenticated admin app.
- Add it to `knownSurfaces`.
- In `actingIdentityFromContext`, detect a valid `AdminAuthContext` after explicit and assistant surfaces, but before normal `AuthContext` derivation.
- Require a non-empty admin session ID. Do not infer admin from an email or request header.
- Update the derivation-order comment.

- [ ] **Step 4: Run the focused tests and confirm they pass**

Run the command from Step 2.

- [ ] **Step 5: Commit the surface change**

```bash
git add server/internal/audit/surface.go server/internal/audit/surface_internal_test.go
git commit -m 'feat(audit): identify the admin acting surface'
```

---

### Task 2: Mask admin actors in customer audit logs and facets

**Files:**

- Modify: `server/internal/auditapi/list_test.go`
- Modify: `server/internal/auditapi/speakeasy_mask_test.go`
- Modify: `server/internal/auditapi/impl.go`
- Modify: `server/internal/audit/queries.sql`
- Regenerate: `server/internal/audit/repo/queries.sql.go`

- [ ] **Step 1: Let audit API fixtures store an acting surface**

Add `actingSurface *string` to `auditLogSeed`. Pass it to `repo.InsertAuditLog` with `conv.PtrToPGTextEmpty`. This is test support only.

- [ ] **Step 2: Add failing customer-mask tests**

In `speakeasy_mask_test.go`, add two tests.

1. `TestAuditService_List_MasksAdminSurfaceActors`:
   - Insert an event for the customer organization.
   - Use a raw OIDC subject that is not a Gram member ID.
   - Store `actor_display_name = "Test Operator"`, an actor slug, and `acting_surface = "admin"`.
   - Require the list result to show `Speakeasy Team` and no actor slug.
   - Keep a non-admin control event and require its name to stay unchanged.

2. `TestAuditService_ListFacets_MasksAdminSurfaceActors`:
   - Insert an equivalent admin event.
   - Require the actor facet to show `Speakeasy Team`.
   - Do not require a Speakeasy organization membership row for the OIDC subject.

- [ ] **Step 3: Run the new tests and confirm the privacy failure**

Run:

```bash
mise run test:server ./internal/auditapi/ -run '^(TestAuditService_List_MasksAdminSurfaceActors|TestAuditService_ListFacets_MasksAdminSurfaceActors)$' -count=1
```

Expected: both responses expose `Test Operator`.

- [ ] **Step 4: Expose admin-surface membership in actor facets**

In `ListAuditActorFacets`:

- Include `acting_surface` in `filtered_logs`.
- Add `BOOL_OR(acting_surface = 'admin') AS is_admin_actor` to `actor_counts`.
- Return `is_admin_actor` with each actor facet row.

Run:

```bash
mise run gen:sqlc-server
```

Do not edit `server/internal/audit/repo/queries.sql.go` by hand.

- [ ] **Step 5: Apply the mask in both customer read paths**

In `auditapi/impl.go`:

- In `List`, mask an event when `log.ActingSurface == string(audit.SurfaceAdmin)`. Set the display name to `audit.SpeakeasyTeamActorLabel` and clear the slug.
- Keep the current Gram-user membership mask for non-admin staff events.
- Apply the admin-surface mask for all customer organizations, including the Speakeasy organization.
- In `ListFacets`, mask a facet when `row.IsAdminActor` is true. Keep the current membership mask for the other user actors.
- Prefer a small shared predicate or helper so the list and facet rules do not drift.

- [ ] **Step 6: Run focused and regression tests**

Run:

```bash
mise run test:server ./internal/auditapi/ -run '^(TestAuditService_List_MasksAdminSurfaceActors|TestAuditService_ListFacets_MasksAdminSurfaceActors|TestAuditService_List_MasksSpeakeasyOrgActors|TestAuditService_ListFacets_MasksSpeakeasyOrgActors|TestAuditService_List_DoesNotMaskSpeakeasyOrgViewingItself)$' -count=1
```

Expected: admin actors are always masked in the customer API. Existing Gram-user mask behavior does not change.

- [ ] **Step 7: Commit the customer-mask change**

```bash
git add server/internal/audit/queries.sql server/internal/audit/repo/queries.sql.go server/internal/auditapi/impl.go server/internal/auditapi/list_test.go server/internal/auditapi/speakeasy_mask_test.go
git commit -m 'fix(audit): mask admin actors in customer activity'
```

---

### Task 3: Store the real admin staff name

**Files:**

- Create: `server/internal/admin/actor_test.go`
- Modify: `server/internal/admin/impl.go`
- Modify: `server/internal/admin/billing.go`
- Modify: `server/internal/admin/chatanalysis_handler.go`
- Modify: `server/internal/admin/extendtrial_test.go`
- Modify: `server/internal/admin/rearmtrial_test.go`
- Modify: `server/internal/admin/billing_test.go`
- Modify: `server/internal/admin/chatanalysis_handler_test.go`

- [ ] **Step 1: Add failing helper tests**

Add table tests for `adminActor`:

- An authenticated admin returns the OIDC subject, the session `Name` as the display name, and the email for private structured logs.
- A blank name uses the email as the display-name fallback.
- A missing admin context keeps the existing system actor and returns no private staff name or email.

Change the helper contract to return `(actor, displayName, operatorEmail)`.

- [ ] **Step 2: Change existing audit integration expectations**

Update the existing admin tests before implementation:

- Extension and re-arm rows store `Test Operator`, not `Speakeasy Team`.
- Extension and re-arm rows store `acting_surface = admin`.
- Chat-analysis settings store `Test Operator` and `acting_surface = admin`.
- Inference-limit scheduling passes `Test Operator`.
- Stripe subscription changes pass `Test Operator`.

Keep assertions that actor IDs use the OIDC subject. Keep assertions that structured logs do not place the email in event snapshots or metadata. Customer privacy is now enforced and tested in Task 2, not by changing the stored name.

- [ ] **Step 3: Run the admin tests and confirm the expected failures**

Run:

```bash
mise run test:server ./internal/admin/ -run '^(TestAdminActor|TestExtendTrial_AuditEntryNamesTheOperator|TestRearmTrial_AuditEntryNamesTheOperator|TestChatAnalysisSettingsDefaultsUpdatesAndAudits|TestSetInferenceKeyMonthlyLimitSchedulesDurableAdminOperation|TestCancelStripeSubscriptionUsesExplicitOrganization)$' -count=1
```

Use the final test names if the old team-label tests are renamed. Expected: stored names are still `Speakeasy Team`.

- [ ] **Step 4: Refactor `adminActor` and all current callers**

In `impl.go`, make `adminActor` return the actor, display name, and email. Select the first non-blank value from `AdminAuthContext.Name` and `AdminAuthContext.Email` for the display name. Keep the email as a separate return value for structured logs.

Update every current caller:

- Trial extension in `impl.go`.
- Trial re-arm in `impl.go`.
- Chat-analysis trigger and settings in `chatanalysis_handler.go`.
- Inference key limit and Stripe subscription actions in `billing.go`.

Pass the real display name to audit writers, billing actors, and durable operation inputs. Remove direct uses of `SpeakeasyTeamActorLabel` from `server/internal/admin`. Update comments that say the stored row must use the team label.

- [ ] **Step 5: Run the focused admin tests**

Run the command from Step 3. Confirm the stored name and direct admin surface assertions pass.

- [ ] **Step 6: Commit the actor-name change**

```bash
git add server/internal/admin/actor_test.go server/internal/admin/impl.go server/internal/admin/billing.go server/internal/admin/chatanalysis_handler.go server/internal/admin/extendtrial_test.go server/internal/admin/rearmtrial_test.go server/internal/admin/billing_test.go server/internal/admin/chatanalysis_handler_test.go
git commit -m 'feat(admin): keep staff names in private audit data'
```

---

### Task 4: Preserve the admin surface across the durable spend-cap activity

**Files:**

- Modify: `server/internal/background/activities/set_openrouter_spend_cap.go`
- Modify: `server/internal/background/activities/set_openrouter_spend_cap_test.go`

- [ ] **Step 1: Add a failing durable-path assertion**

In `TestSetOpenRouterSpendCapAdminBypassesBillingAndTrialPolicy`, read the written audit row with `audittest.LatestAuditLogByAction`. Require `ActingSurface` to be `admin`.

Also add or keep a non-admin spend-cap test that requires its existing surface behavior. Do not label every spend-cap workflow as admin.

- [ ] **Step 2: Run the focused test and confirm the failure**

Run:

```bash
mise run test:server ./internal/background/activities/ -run '^TestSetOpenRouterSpendCapAdminBypassesBillingAndTrialPolicy$' -count=1
```

Expected: the activity audit row has `unknown` because Temporal does not preserve request context.

- [ ] **Step 3: Mark only the admin activity path**

In `SetOpenRouterSpendCap.Do`, when `args.BypassPolicy` is true, set the acting surface on the activity context to `admin` before the audit logger runs. Use `contextvalues.SetActingSurface` and `audit.SurfaceAdmin`.

Do not accept an arbitrary surface from a workflow payload. `BypassPolicy` already identifies the admin-only workflow, and the audit package still enforces the closed surface set.

- [ ] **Step 4: Run the focused activity test**

Run the command from Step 2.

- [ ] **Step 5: Commit the durable-path change**

```bash
git add server/internal/background/activities/set_openrouter_spend_cap.go server/internal/background/activities/set_openrouter_spend_cap_test.go
git commit -m 'fix(audit): keep admin surface through spend-cap work'
```

---

### Task 5: Add release notes and verify the complete change

**Files:**

- Create: `.changeset/admin-audit-acting-surface.md`

- [ ] **Step 1: Add a server patch changeset**

State that new admin actions keep the staff name for the private Activity view, while customer audit responses continue to show `Speakeasy Team`. State that old rows are not backfilled.

- [ ] **Step 2: Format changed files**

Run:

```bash
hk fix
```

Review the output. Revert changes to files outside this plan.

- [ ] **Step 3: Run the relevant package tests**

Run:

```bash
mise run test:server ./internal/audit/ ./internal/auditapi/ ./internal/admin/ ./internal/background/activities/
```

- [ ] **Step 4: Verify generated SQL and the diff**

Run:

```bash
mise run gen:sqlc-server
git diff --check
git status --short
```

Confirm that the second SQL generation produces no new diff. Confirm that unrelated untracked files are not staged.

- [ ] **Step 5: Commit the changeset and final generated state**

```bash
git add .changeset/admin-audit-acting-surface.md
git commit -m 'chore: document admin audit attribution'
```

- [ ] **Step 6: Request review**

Use `verification-before-completion`, then `requesting-code-review`. Review must check these privacy invariants:

- The database stores the real name for new admin events.
- The customer list and actor facets never return that name.
- The private admin API can read the stored name in AGE-3343.
- Old rows need no backfill.
- Only authenticated admin paths receive the `admin` surface.
