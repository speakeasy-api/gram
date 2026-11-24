# Internal Organizations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let staff mark an organization as internal, list and filter internal organizations in the admin dashboard and Admin MCP, and mirror the flag to PostHog.

**Architecture:** A boolean column on `organization_metadata` is the source of truth. The staff admin API reads, writes and filters on it; the admin dashboard and Admin MCP are thin consumers of that API. Every write of the flag is followed by a best-effort PostHog `GroupIdentify` on the organization group.

**Tech Stack:** Postgres + Atlas migrations, sqlc, Goa (admin service), Go MCP SDK (Admin MCP), React + TanStack Table/Router + shadcn (client/admin), PostHog Go client.

**Spec:** `docs/superpowers/specs/2026-11-24-internal-organizations-design.md`

## Global Constraints

- Column: `organization_metadata.internal boolean NOT NULL DEFAULT FALSE`.
- API field name is `internal`; list filter is `internal_status` with values `all` (default), `internal`, `external`.
- PostHog call is exactly `GroupIdentify(ctx, "organization", <org slug>, map[string]any{"is_internal": <bool>})`. The group key is the slug, never the id.
- The flag grants nothing: no code path may branch on it for access, limits or rollout.
- No existing page, query or report changes its default result set. `internal_status` defaults to `all` everywhere.
- Flag changes go to the server log only. Do not write `audit_logs` rows or publish outbox events for them.
- The migration ships in its own PR (`postgresql` skill, "Database migrations"). Migration files and `atlas.sum` come only from `mise run db:diff`.
- Write "Speakeasy" as the product name in all new text (`mise run lint:brand` enforces it).
- No customer names, organization ids or slugs in any committed file. Fixtures use invented names.
- Files under `server/gen/`, `server/migrations/` and `client/admin/src/sdk/` are generated; never hand-edit.
- Commit locally after each task. Do not push or open a PR until the user says so.

**Skills to activate while executing:** `postgresql` (Task 1, and any `queries.sql` edit), `gram-management-api` and `golang` (Tasks 2-4), `maintaining-admin-mcp` (Task 5), `admin-shadcn` (Tasks 6-7), `pull-request` (Task 8).

## PR Stack

The work ships as a stack of four PRs, each branch based on the one before
it. This section sets the execution order and overrides the task numbering
below where they differ.

| PR  | Branch suffix | Tasks         | Contents                                                            | Depends on             |
| --- | ------------- | ------------- | ------------------------------------------------------------------- | ---------------------- |
| 1   | `mig`         | Task 1        | Column and migration only                                           | -                      |
| 2   | `api`         | Tasks 2, 3, 4 | Admin API reads, writes and filters on the flag; PostHog mirror     | 1, merged and deployed |
| 3   | `dashboard`   | Tasks 6, 7    | Overview switch, muted rows, Internal filter in the admin dashboard | 2                      |
| 4   | `admin-mcp`   | Task 5        | Flag and filter in the Admin MCP                                    | 2                      |

Rules for the stack:

- Every PR builds, passes its tests and leaves the product working on its own.
- Within a PR, keep one commit per task so a reviewer can read it in steps.
- PR 2 carries the regenerated server code and the regenerated admin SDK
  (`mise run gen:sdk`) for both Goa design edits, because CI checks that the
  SDK matches the design. That moves the SDK regeneration out of Task 6; run
  it once, after Task 3.
- Each PR with a user-visible change carries its own one-sentence changeset.
  Task 8 Step 1 is dropped; Task 8 Step 2 runs on the top of the stack.
- PR 1 uses the `mig:` title prefix and must be merged and deployed before
  PR 2 merges, per the `postgresql` skill. `mise run db:mig-pr` is not needed
  because the migration already sits alone at the bottom of the stack.

**Recorded assessments (no work needed):**

- Platform MCP: no change. The flag is staff-only and not visible to customers.
- Command K: no change. No customer-dashboard page, route or named resource is added.
- Demo seed: no change. The surface is the staff admin dashboard and the demo organization is not internal.

## Review Focus

1. Marking an organization id that does not exist returns not-found and sends nothing to PostHog. (Task 2, Task 4)
2. PostHog is unconfigured (nil identifier) or returns an error: the write still succeeds and the response shows the new value. (Task 4)
3. Un-marking sends `is_internal: false` explicitly, not an omitted property. (Task 4)
4. `internal_status=` present but empty is rejected, not treated as `all`; an unknown value is rejected. (Task 3)
5. `internal_status` combined with other filters: `total` equals the number of matching rows, and a disabled internal organization still appears under `internal`. (Task 3)

---

### Task 1: Schema and migration

**Files:**

- Modify: `server/database/schema.sql` (the `organization_metadata` table, after `adoption_dashboard_url`)
- Create (generated): `server/migrations/<timestamp>_add-organization-internal.sql`, updated `server/migrations/atlas.sum`
- Modify (generated): sqlc models that select `*` from `organization_metadata`

**Interfaces:**

- Produces: column `organization_metadata.internal boolean NOT NULL DEFAULT FALSE`; sqlc field `Internal bool` on `OrganizationMetadatum`-style models.

- [ ] **Step 1: Add the column to `schema.sql`**

```sql
  -- Staff-set label for organizations owned by Speakeasy employees for testing
  -- and dogfooding, so analytics can exclude them. A label only: it grants no
  -- access and changes no limits.
  internal boolean NOT NULL DEFAULT FALSE,
```

- [ ] **Step 2: Generate the migration**

Run: `mise run db:diff add-organization-internal`
Expected: one new migration containing a single `ALTER TABLE "organization_metadata" ADD COLUMN "internal" boolean NOT NULL DEFAULT false;` and an updated `atlas.sum`.

- [ ] **Step 3: Apply and lint**

Run: `mise run db:migrate && mise lint:migrations`
Expected: migration applied, lint clean.

- [ ] **Step 4: Regenerate sqlc and build**

Run: `mise run gen:sqlc-server && mise build:server`
Expected: build succeeds; generated models gain `Internal bool`.

- [ ] **Step 5: Commit**

```bash
git add server/database/schema.sql server/migrations server/internal
git commit -m "mig: add internal flag to organization_metadata"
```

---

### Task 2: Admin API reads and writes the flag

**Files:**

- Modify: `server/design/admin/design.go` (`AdminOrganization` type; `updateOrganization` payload and description)
- Modify: `server/internal/admin/queries.sql` (`AdminGetOrganization`, `AdminListOrganizations` row columns; new `AdminSetOrganizationInternal`)
- Create: `server/internal/admin/organization_internal.go`
- Modify: `server/internal/admin/impl.go` (`UpdateOrganization`, `adminOrganizationFromGetRow`, `adminOrganizationFromRow`)
- Modify: `server/internal/admin/listorganizations_test.go` (`orgFixture` gains `internal bool`; `seedOrg` writes it)
- Test: `server/internal/admin/organization_internal_test.go`

**Interfaces:**

- Consumes: column from Task 1.
- Produces:
  - Goa: `AdminOrganization.internal` (Boolean, required); `UpdateOrganizationPayload.Internal *bool`.
  - SQL: `AdminSetOrganizationInternal(ctx, AdminSetOrganizationInternalParams{ID string, Internal bool}) (slug string, err error)`, which returns `pgx.ErrNoRows` for an unknown id.
  - Go: `func (s *Service) setOrganizationInternal(ctx context.Context, id string, internal bool) (slug string, err error)` in `organization_internal.go`; returns `oops.C(oops.CodeNotFound)` for an unknown id.

- [ ] **Step 1: Write the failing tests** in `organization_internal_test.go`, using `newTestAdminService` and `seedOrg` as `adoption_dashboard_test.go` does.

```go
func TestUpdateOrganization_SetsAndClearsInternal(t *testing.T) {
	// seed org_internal_a (internal: false)
	// UpdateOrganization{ID, Internal: new(true)}  -> res.Internal == true
	// GetOrganization                               -> Internal == true
	// ListOrganizations{Q: slug}                    -> row.Internal == true
	// UpdateOrganization{ID, Internal: new(false)} -> res.Internal == false
}

func TestUpdateOrganization_InternalLeavesOtherFieldsAlone(t *testing.T) {
	// seed enterprise, whitelisted org; set Internal true
	// res.AccountType == "enterprise" && res.Whitelisted == true
}

func TestUpdateOrganization_InternalUnknownOrganization(t *testing.T) {
	// UpdateOrganization{ID: "org_missing", Internal: new(true)}
	// error code == oops.CodeNotFound
}

func TestUpdateOrganization_InternalCannotBeCombined(t *testing.T) {
	// Internal with AccountType, and Internal with Whitelisted
	// each: error code == oops.CodeBadRequest, and GetOrganization shows nothing changed
}

func TestUpdateOrganization_RequiresAField(t *testing.T) {
	// UpdateOrganization{ID} -> CodeBadRequest,
	// message "at least one of account_type, whitelisted or internal must be supplied"
}

func TestUpdateOrganization_InternalWritesNoAuditLog(t *testing.T) {
	// count audit_logs rows for the organization before and after; equal
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `mise run test:server ./internal/admin/ -run 'TestUpdateOrganization_(SetsAndClearsInternal|Internal|RequiresAField)'`
Expected: compile failure (`Internal` undefined).

- [ ] **Step 3: Edit the Goa design**

`AdminOrganization`: add `"internal"` to `Required(...)` and
`Attribute("internal", Boolean, "Whether staff have marked this organization as internal: owned by Speakeasy employees for testing or dogfooding. A label for analytics; it grants nothing.")`.

`updateOrganization`: description becomes "Updates admin-managed fields on an organization. At least one of account_type, whitelisted or internal must be supplied. internal must be sent on its own." Add
`Attribute("internal", Boolean, "New internal flag. Cannot be combined with account_type or whitelisted.")`.

- [ ] **Step 4: Edit the queries**

Add `om.internal` to the selected columns of `AdminGetOrganization` and to both the `filtered` CTE and the outer select of `AdminListOrganizations`. Add:

```sql
-- name: AdminSetOrganizationInternal :one
-- Keyed on id alone, like every other admin write. Returns the slug because
-- the PostHog organization group is keyed by it.
UPDATE organization_metadata
SET
    internal = sqlc.arg('internal')::boolean,
    updated_at = clock_timestamp()
WHERE id = sqlc.arg('id')::text
RETURNING slug;
```

- [ ] **Step 5: Regenerate**

Run: `mise run gen:goa-server && mise run gen:sqlc-server`

- [ ] **Step 6: Implement**

- `setOrganizationInternal` in `organization_internal.go`: runs the query, maps `pgx.ErrNoRows` to not-found, and logs at info with `attr.SlogOrganizationID` and `attr.SlogUserID(actor.ID)` (actor from `adminActor(ctx)`), message `"admin marked organization internal"` or `"admin unmarked organization internal"`.
- `UpdateOrganization`: the empty-payload check and message cover `Internal`. When `Internal != nil` and either other field is set, return `oops.E(oops.CodeBadRequest, nil, "internal cannot be combined with account_type or whitelisted")`. When only `Internal` is set, call `setOrganizationInternal`, then `readOrganizationAfterWrite(ctx, payload.ID, "fetch organization after internal change")`.
- Both row mappers set `Internal: row.Internal`.
- `seedOrg` inserts `orgFixture.internal`.

- [ ] **Step 7: Run the tests**

Run: `mise run test:server ./internal/admin/`
Expected: PASS, including the pre-existing suite.

- [ ] **Step 8: Commit**

```bash
git add server/design server/gen server/internal/admin
git commit -m "feat: let staff mark an organization as internal"
```

---

### Task 3: Filter the organization list by internal status

**Files:**

- Modify: `server/design/admin/design.go` (`listOrganizations` payload and HTTP params)
- Modify: `server/internal/admin/queries.sql` (`AdminListOrganizations`, `AdminCountOrganizations`)
- Modify: `server/internal/admin/listorganizationsbounds.go`, `server/internal/admin/impl.go` (`rejectEmptyOrganizationStatus`; both query param structs near the `DisabledStatus: bounds.disabledStatus` lines)
- Test: `server/internal/admin/listorganizationsfilters_test.go`, `server/internal/admin/listorganizationsbounds_test.go`

**Interfaces:**

- Consumes: `Internal` on list rows (Task 2).
- Produces: `ListOrganizationsPayload.InternalStatus *string`; HTTP query param `internal_status`; sqlc param `InternalStatus string` on both list and count params.

- [ ] **Step 1: Write the failing tests**

```go
func TestListOrganizations_InternalStatusFilter(t *testing.T) {
	// seed: cust_a (external), cust_b (external, disabled), int_a (internal), int_b (internal, disabled)
	// nil and "all"  -> 4 rows, Total 4
	// "internal"     -> {int_a, int_b}, Total 2   (the disabled one is included)
	// "external"     -> {cust_a, cust_b}, Total 2
	// "internal" + DisabledStatus "active" -> {int_a}, Total 1
}

func TestListOrganizations_InternalStatusRejectsUnknown(t *testing.T) {
	// InternalStatus "yes" -> CodeInvalid, "internal_status must be all, internal, or external"
}

func TestListOrganizations_InternalStatusRejectsEmptyQueryValue(t *testing.T) {
	// HTTP GET /admin/organizations.list?internal_status=  -> 4xx with the same message,
	// written like the existing empty disabled_status test
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `mise run test:server ./internal/admin/ -run 'TestListOrganizations_InternalStatus'`
Expected: compile failure (`InternalStatus` undefined).

- [ ] **Step 3: Design** — add to `listOrganizations`:

`Attribute("internal_status", String, "Internal label: all (default), internal (marked internal by staff), or external (not marked). Applies even to exact ID matches.", func() { Enum("all", "internal", "external") })` and `Param("internal_status")` after `disabled_status`.

- [ ] **Step 4: Queries** — add this arm next to the `disabled_status` arm in both `AdminListOrganizations` and `AdminCountOrganizations`, keeping the two identical:

```sql
        AND (
            sqlc.arg('internal_status')::text = 'all'
            OR (sqlc.arg('internal_status')::text = 'internal' AND om.internal)
            OR (sqlc.arg('internal_status')::text = 'external' AND NOT om.internal)
        )
```

- [ ] **Step 5: Regenerate** — `mise run gen:goa-server && mise run gen:sqlc-server`

- [ ] **Step 6: Implement** — `bounds.internalStatus` defaults to `"all"` and is validated exactly like `disabledStatus`, with the error text from the test. `rejectEmptyOrganizationStatus` also rejects a present-but-empty `internal_status`. Pass `InternalStatus: bounds.internalStatus` to both queries.

- [ ] **Step 7: Run the tests**

Run: `mise run test:server ./internal/admin/`
Expected: PASS. `organization_query_plans_test.go` must still pass; if a plan assertion changes, report it rather than loosening the assertion.

- [ ] **Step 8: Commit**

```bash
git add server/design server/gen server/internal/admin
git commit -m "feat: filter the admin organization list by internal status"
```

---

### Task 4: Mirror the flag to PostHog

**Files:**

- Modify: `server/internal/admin/organization_internal.go`, `server/internal/admin/impl.go` (`Service` field)
- Modify: `server/cmd/gram/admin.go` (after `admin.NewService(...)`; grep `admin.NewService(` for any other construction site and wire it the same way)
- Test: `server/internal/admin/organization_internal_test.go`

**Interfaces:**

- Consumes: `setOrganizationInternal` and its returned slug (Task 2).
- Produces:

```go
// OrganizationGroupIdentifier sets properties on the PostHog organization group.
type OrganizationGroupIdentifier interface {
	GroupIdentify(ctx context.Context, groupType string, groupKey string, groupProperties map[string]any) error
}

// SetOrganizationGroupIdentifier wires the PostHog client. Nil leaves the
// internal flag unmirrored.
func (s *Service) SetOrganizationGroupIdentifier(identifier OrganizationGroupIdentifier)
```

`*posthog.Posthog` already satisfies the interface.

- [ ] **Step 1: Write the failing tests** with a recording fake that stores each call and can be told to return an error.

```go
func TestUpdateOrganization_InternalMirrorsToPostHog(t *testing.T) {
	// seed slug "mirror-co"; set true, then false
	// calls == [{"organization", "mirror-co", {"is_internal": true}},
	//           {"organization", "mirror-co", {"is_internal": false}}]
}

func TestUpdateOrganization_InternalResendsUnchangedValue(t *testing.T) {
	// set true twice -> two calls, both is_internal true
}

func TestUpdateOrganization_InternalSurvivesPostHogFailure(t *testing.T) {
	// fake returns an error -> UpdateOrganization returns no error, res.Internal == true
}

func TestUpdateOrganization_InternalWithoutPostHog(t *testing.T) {
	// no identifier set -> no panic, res.Internal == true
}

func TestUpdateOrganization_InternalUnknownOrganizationSendsNothing(t *testing.T) {
	// unknown id -> CodeNotFound, zero calls
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `mise run test:server ./internal/admin/ -run 'TestUpdateOrganization_Internal'`
Expected: compile failure (`SetOrganizationGroupIdentifier` undefined).

- [ ] **Step 3: Implement** — after the database write succeeds, and on every such write whether or not the value changed, call the identifier with the constants from Global Constraints. On error, log with `s.logger.ErrorContext` plus `attr.SlogError` and `attr.SlogOrganizationID`, and continue. Skip when the identifier is nil.

- [ ] **Step 4: Wire it** — in `server/cmd/gram/admin.go`, call `adminService.SetOrganizationGroupIdentifier(posthogClient)` after construction.

- [ ] **Step 5: Run the tests and build**

Run: `mise run test:server ./internal/admin/ && mise build:server`
Expected: PASS and a clean build.

- [ ] **Step 6: Commit**

```bash
git add server/internal/admin server/cmd/gram/admin.go
git commit -m "feat: mirror the internal organization flag to PostHog"
```

---

### Task 5: Admin MCP

**Files:**

- Modify: `server/internal/adminmcp/tool_organizations.go`, `server/internal/adminmcp/organization_filters.go`
- Test: `server/internal/adminmcp/tool_organizations_test.go`

**Interfaces:**

- Consumes: `gen.AdminOrganization.Internal`, `gen.ListOrganizationsPayload.InternalStatus` (Tasks 2-3).
- Produces: `FindOrganizationsInput.InternalStatus *string` (`json:"internal_status,omitempty"`); `OrganizationMatch.Internal bool` (`json:"internal"`); `OrganizationSummary.Internal bool` (`json:"internal"`).

- [ ] **Step 1: Write the failing tests**

```go
func TestFindOrganizationsInternalStatus(t *testing.T) {
	// input {InternalStatus: "internal"} with no query and no other filter is accepted,
	//   and the payload passed to the reader has InternalStatus == "internal"
	// each match carries Internal from the row
	// input {InternalStatus: "maybe"} -> error "internal_status must be all, internal or external"
}

func TestOrganizationSummaryInternal(t *testing.T) {
	// reader returns Internal true -> summary.Internal == true
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `mise run test:server ./internal/adminmcp/ -run 'Internal'`
Expected: compile failure.

- [ ] **Step 3: Implement**

- `InternalStatus` jsonschema text: `"Internal label: all, internal or external; internal organizations belong to Speakeasy employees. Omit instead of supplying an empty value"`.
- In `organizationSearchPayload`, a non-nil `InternalStatus` counts as an explicit filter, is validated against the three values, and is copied to the payload.
- `find_organizations` description: append "Each result says whether staff marked the organization internal, meaning it belongs to Speakeasy employees for testing or dogfooding. Filter with internal_status, and exclude internal organizations from customer analysis unless asked otherwise."
- `get_organization_summary` description: append "Also says whether the organization is marked internal."

- [ ] **Step 4: Run the package tests**

Run: `mise run test:server ./internal/adminmcp/`
Expected: PASS. If a tool-manifest or description snapshot test fails, update the snapshot the way that test documents.

- [ ] **Step 5: Commit**

```bash
git add server/internal/adminmcp
git commit -m "feat: expose the internal organization flag in the Admin MCP"
```

---

### Task 6: Admin dashboard — Internal switch on the organization overview

**Files:**

- Generated: `client/admin/src/sdk/` via `mise run gen:sdk`
- Modify: `client/admin/src/lib/gramAdminApi.ts` (`AdminOrganization`, list params type, update body type)
- Modify: `client/admin/src/pages/organization/Overview.tsx`
- Test: the existing Overview test file beside it (create `Overview.test.tsx` only if none covers the Whitelisted switch; otherwise extend that one), `client/admin/src/lib/gramAdminApi.test.ts`

**Interfaces:**

- Consumes: API from Tasks 2-3.
- Produces, in `gramAdminApi.ts`: `AdminOrganization.internal: boolean`; list params `internal_status?: "all" | "internal" | "external"`; update body `internal?: boolean`.

- [ ] **Step 1: Regenerate the SDK** — `mise run gen:sdk`

- [ ] **Step 2: Write the failing tests**

```tsx
it("marks an organization internal after confirmation", async () => {
  // render Overview with org.internal === false
  // click the switch labelled "Internal"; confirm dialog text contains "Internal: No → Yes"
  // confirm -> update request body is exactly { id, internal: true }
});

it("describes the current internal state", () => {
  // internal true  -> "Hidden from customer analytics"
  // internal false -> "Counts as a customer"
});
```

- [ ] **Step 3: Run to verify they fail**

Run: `aube run -F admin test src/pages/organization`
Expected: FAIL, no switch labelled "Internal".

- [ ] **Step 4: Implement** — extend `FactChange` with `| { internal: boolean }`. Add `<Row label="Internal">` directly after the Whitelisted row, built the same way: a `Switch` with its own `internalControl` ref, `checked={org.internal}`, the same `disabled` expression, and `commit({ internal: v }, \`Internal: ${yesNo(org.internal)} → ${yesNo(v)}\`, internalControl)`. Helper text as in the test.

- [ ] **Step 5: Run tests and type-check**

Run: `aube run -F admin test src/pages/organization src/lib && aube run -F admin type-check`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add client/admin
git commit -m "feat: add an Internal switch to the admin organization overview"
```

---

### Task 7: Admin dashboard — muted rows and the Internal filter

**Files:**

- Modify: `client/admin/src/lib/organizationFilters.ts`, `client/admin/src/lib/organizationSearch.ts`
- Modify: `client/admin/src/pages/organizations/FilterSheet.tsx`, `Toolbar.tsx`, `applyFilters.ts`, `index.tsx`, `columns.tsx`
- Test: `client/admin/src/lib/organizationFilters.test.ts`, `client/admin/src/lib/organizationSearch.test.ts`, `client/admin/src/pages/organizations/index.test.tsx`, `FilterSheet.test.tsx`

**Interfaces:**

- Consumes: `AdminOrganization.internal` and `internal_status` (Task 6).
- Produces, in `organizationFilters.ts`, each mirroring its `disabled` counterpart:

```ts
export const INTERNAL_STATES = ["internal", "external"] as const;
export type InternalState = (typeof INTERNAL_STATES)[number];
// FILTER_GROUP_KEYS gains "internal"; NO_FILTERS gains internal: []
// FilterSearch gains internalStatus?: "all" | InternalState
export function internalSelection(search: FilterSearch): string[];
export function internalParams(search: FilterSearch): {
  internal_status: "all" | "internal" | "external";
};
```

- [ ] **Step 1: Write the failing tests**

```ts
// organizationFilters.test.ts
it("maps the internal filter to the request", () => {
  expect(internalParams({})).toEqual({ internal_status: "all" });
  expect(internalParams({ internalStatus: "internal" })).toEqual({
    internal_status: "internal",
  });
  expect(internalParams({ internalStatus: "external" })).toEqual({
    internal_status: "external",
  });
});
it("writes only a chosen internal status to the URL", () => {
  expect(
    filtersToSearch({ ...NO_FILTERS, internal: ["external"] }).internalStatus,
  ).toBe("external");
  expect(filtersToSearch(NO_FILTERS).internalStatus).toBeUndefined();
});
```

```ts
// organizationSearch.test.ts
// an unknown internalStatus in the URL is dropped, like an unknown disabledStatus
```

```tsx
// index.test.tsx
it("mutes internal rows and names them for screen readers", async () => {
  // rows: one with internal true, one false
  // internal row element has class "text-muted-foreground"; the other does not
  // within the internal row, getByText("Internal") has class "sr-only"
  // the name link's accessible name is still exactly the organization name
});
it("requests the chosen internal status", async () => {
  // route search internalStatus=external -> list request has internal_status=external
  // toolbar shows a chip "Internal: excluded"; clearing it requests internal_status=all
});
```

```tsx
// FilterSheet.test.tsx
// the "Internal" select offers "All organizations", "Internal only", "Exclude internal";
// choosing "Internal only" and applying calls onApply with internal: ["internal"]
```

- [ ] **Step 2: Run to verify they fail**

Run: `aube run -F admin test src/lib/organizationFilters.test.ts src/lib/organizationSearch.test.ts src/pages/organizations`
Expected: FAIL.

- [ ] **Step 3: Implement**

- Filter group, last in `FILTER_GROUPS`: `{ key: "internal", label: "Internal", emptyLabel: "All organizations", allLabel: "All organizations" }` with option labels `internal: "Internal only"`, `external: "Exclude internal"`.
- `FilterSheet.tsx`: render the `internal` group with the same single-`Select` branch the `disabled` group uses.
- `Toolbar.tsx`: `filters.internal` comes from `internalSelection(search)`; chip text is `"Internal: only"` for `internal` and `"Internal: excluded"` for `external`.
- `organizationSearch.ts` and `applyFilters.ts`: carry `internalStatus` wherever `disabledStatus` is carried.
- `index.tsx`: spread `internalParams(search)` into the list request and its cache key; add `row.original.internal && "text-muted-foreground"` to the `Table.Row` `className`.
- `columns.tsx`: in the name cell, after the `Link`, render `<span className="sr-only">Internal</span>` when `row.original.internal`. Keep it outside the link.

- [ ] **Step 4: Run tests, type-check, lint**

Run: `aube run -F admin test && aube run -F admin lint`
Expected: PASS.

- [ ] **Step 5: Look at it**

Wake the stack (`mise run wake`), mark one local organization internal from its overview page, then check the organizations list: the row is muted, the filter sheet has the Internal select, the chip appears and clears. If badges or links inside the muted row keep full-strength colour, mute them in the cell renderers and re-run Step 4.

- [ ] **Step 6: Commit**

```bash
git add client/admin
git commit -m "feat: mute internal organizations in the admin list and filter by them"
```

---

### Task 8: Changeset, full verification, and the migration split

**Files:**

- Create: `.changeset/<name>.md` (follow `.changeset/README.md` for package names and bump level)

- [ ] **Step 1: Add the changeset** — one sentence: "Staff can mark an organization as internal in the admin dashboard, filter the organization list by it, and read it through the Admin MCP; the flag is mirrored to PostHog."

- [ ] **Step 2: Full verification**

Run: `hk fix && mise lint:server && mise run test:server ./internal/admin/ ./internal/adminmcp/ ./internal/thirdparty/posthog/ && aube run -F admin lint && aube run -F admin test && mise run gen:sdk --check && mise run lint:brand`
Expected: all clean. Paste the output into the handoff.

- [ ] **Step 3: Commit**

```bash
git add .changeset
git commit -m "chore: add changeset for internal organizations"
```

- [ ] **Step 4: Stop and hand back.** Report what was verified and remind the user of the two things only they can do: approve pushing (which starts with `mise run db:mig-pr` so the migration ships first in its own PR), and add "organization `is_internal` is not `true`" to the PostHog project's internal and test accounts filter.
