# AGE-3343 Admin Organization Activity API Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `subagent-driven-development` or `executing-plans` to implement this plan task by task.

**Goal:** Add an admin-only API that lists all Activity for one explicit organization, with stable cursor pagination and private actor names.

**Architecture:** Add `listOrganizationActivity` to the existing admin Goa service. Validate the target with the strict ID-only organization lookup before reading audit rows. Reuse `ListAuditLogs` with an `include_assistant_events` parameter. The admin service passes `true`; the customer service passes `false`. Move the existing cursor codec into the neutral audit package. Map audit rows directly in the admin service, without the customer masking step. Normalize the stored system actor to the response label `System`. Audit sequence is the authoritative event order: a later insert has a larger sequence, so `seq DESC` gives newest activity first and remains stable when timestamps are equal.

**Dependency:** Implement on top of AGE-3342. That change adds `acting_surface = admin`, stores the private operator name, and keeps customer masking separate.

**Tech Stack:** Go, Goa, PostgreSQL/sqlc, Testify

**Required skills:** `test-driven-development`, `postgresql`, `verification-before-completion`

---

### Task 1: Define the admin Activity API contract

**Files:**

- Modify: `server/design/admin/design.go`
- Regenerate: `server/gen/admin/**`
- Regenerate: `server/gen/http/admin/**`
- Regenerate: `server/gen/http/openapi3.yaml`
- Modify: `server/internal/admin/impl.go`

- [ ] **Step 1: Add the Goa method and response types**

Add `listOrganizationActivity` to the admin service. Import and use `auditlogs.AuditLog` directly in an admin list result with required `logs` and optional `next_cursor`. Do not copy the audit-log type.

The payload must use `security.AdminAuthPayload()` and require `organization_id`. Add optional `cursor`. Do not add `id_or_slug`, project filters, search, facets, export, or an active-organization fallback.

Use:

- `GET /admin/organization.activity`
- query parameters `organization_id` and `cursor`
- OpenAPI operation ID `adminListOrganizationActivity`

The dashboard route in AGE-3344 remains `/organizations/:idOrSlug/activity`; that route is not the server API path.

- [ ] **Step 2: Generate Goa code**

Run:

```bash
mise run gen:goa-server
```

Do not edit generated files by hand.

- [ ] **Step 3: Add the smallest compiling method stub**

Add `ListOrganizationActivity` to `server/internal/admin/impl.go` with the generated signature. Return an explicit not-implemented error. This stub lets the behavioral tests compile and fail for their own assertions. Do not add lookup, query, mapping, or pagination behavior yet.

Run:

```bash
mise run test:server ./internal/admin/ -run '^TestListOrganizationActivity' -count=1
```

Expected: the package compiles and reports no matching tests.

---

### Task 2: Share cursor parsing and let the admin query include all events

**Files:**

- Create: `server/internal/audit/cursor.go`
- Create: `server/internal/audit/cursor_test.go`
- Modify: `server/internal/audit/queries.sql`
- Regenerate: `server/internal/audit/repo/queries.sql.go`
- Modify: `server/internal/auditapi/impl.go`
- Modify: `server/internal/auditapi/list_test.go`
- Create: `server/internal/audit/list_test.go`

- [ ] **Step 1: Add failing cursor tests**

Add table tests for exported audit cursor helpers. Require a sequence and row ID to round-trip through the existing base64url format. Require malformed base64, a missing separator, and a non-numeric sequence to fail. Keep the row ID in the encoded value to preserve the current opaque cursor format.

- [ ] **Step 2: Run the cursor tests and confirm they fail**

Run:

```bash
mise run test:server ./internal/audit/ -run '^TestAuditCursor' -count=1
```

- [ ] **Step 3: Move the cursor codec into the audit package**

Move the current `encodeCursor` and `decodeCursor` behavior from `server/internal/auditapi/impl.go` to exported helpers in `server/internal/audit/cursor.go`. Update the customer audit service to use the shared helpers. Do not change the wire format.

- [ ] **Step 4: Add failing query tests**

Add `TestListAuditLogs_IncludeAssistantEvents` in `server/internal/audit/list_test.go`. Insert one assistant event and one non-assistant event for the same organization. Require `IncludeAssistantEvents = false` to return only the non-assistant event. Require `IncludeAssistantEvents = true` with no subject filter to return both events. Run the focused test and confirm the generated parameter does not exist.

- [ ] **Step 5: Add the explicit all-events query option**

Add the required sqlc boolean parameter `include_assistant_events` to `ListAuditLogs`. Change only the default assistant exclusion arm. Update the query comment to state that the private admin caller can include assistant events without a subject filter.

- customer callers pass `false` and keep the current behavior;
- the private admin caller passes `true` and receives assistant events with every other subject type.

Do not remove the customer exclusion. Do not create a second copy of the audit query.

Run:

```bash
mise run gen:sqlc-server
```

- [ ] **Step 6: Run cursor, query, and customer regression tests**

Run:

```bash
mise run test:server ./internal/audit/ ./internal/auditapi/ -run '^(TestAuditCursor|TestListAuditLogs_IncludeAssistantEvents|TestAuditService_List_ExcludesAssistantEventsByDefault|TestAuditService_List_Pagination|TestAuditService_List_InvalidCursor)$' -count=1
```

Expected: the existing customer feed still excludes assistant events by default and keeps its cursor behavior.

---

### Task 3: Implement the organization-scoped private Activity list

**Files:**

- Create: `server/internal/admin/listorganizationactivity_test.go`
- Modify: `server/internal/admin/impl.go`

- [ ] **Step 1: Add failing HTTP authentication tests**

Mount `Attach` behind `SessionMiddleware`, as production does. Test all three cases against `/admin/organization.activity?organization_id=<ORG_ID>`:

1. No admin cookie returns HTTP 401.
2. A normal tenant credential without an admin session returns HTTP 401.
3. A valid admin session reaches the generated endpoint. It fails only because the service stub is not implemented.

Also omit `organization_id` and require HTTP payload validation to reject the request. Use placeholder credentials and organization IDs. Do not add another authorization system inside the service method.

- [ ] **Step 2: Add failing organization and pagination tests**

Create direct service tests for `ListOrganizationActivity`:

1. A syntactically valid but nonexistent organization ID returns `oops.CodeNotFound`.
2. A real organization slug supplied as `organization_id` returns not found. This pins ID-only lookup.
3. An unrelated active organization in normal auth context is never used as a fallback.
4. An existing organization with no audit rows returns an empty list and no cursor.
5. Rows from another organization never appear, even when they are newer.
6. Named trial, non-trial project, and assistant fixtures all appear when the admin call supplies no action or subject filters.
7. Rows with distinct creation times return newest first. Rows with the same creation time return in descending audit sequence order.
8. Fifty-one rows return 50 rows and a cursor on page one, then the final row without duplicates or gaps on page two. Use equal timestamps to prove stable sequence ordering.
9. A malformed cursor returns `oops.CodeBadRequest`.

Insert audit fixtures through `audit/repo.InsertAuditLog`. Use placeholders only.

- [ ] **Step 3: Add failing full-response and actor tests**

Require the response to preserve:

- event ID, action, and creation time;
- project ID and slug;
- actor ID, type, display name, and slug;
- subject ID, type, display name, and slug;
- before and after snapshots;
- metadata;
- acting surface and acting client ID.

Add these privacy and compatibility cases:

- An `admin` row with `actor_display_name = "Test Operator"` returns `Test Operator`, not `Speakeasy Team`.
- A legacy null acting surface returns `unknown`.
- An automated row with `actor_id = "system"` returns the clear display label `System` while retaining the actor ID.

- [ ] **Step 4: Run the focused tests and confirm failure**

Run:

```bash
mise run test:server ./internal/admin/ -run '^TestListOrganizationActivity' -count=1
```

Expected: the new tests execute against the compiling stub and fail on not-implemented behavior.

- [ ] **Step 5: Implement the smallest service method**

In `ListOrganizationActivity`:

1. Call `AdminGetOrganization` with `ID = payload.OrganizationID` and `AllowSlug = false`.
2. Map `pgx.ErrNoRows` to `oops.CodeNotFound`. Map other lookup failures to `oops.CodeUnexpected` with `lookup organization for activity`, log them, and include the organization ID as structured context.
3. Decode a non-empty cursor with the shared audit helper and map errors to `oops.CodeBadRequest`.
4. Call `auditrepo.ListAuditLogs` with only the explicit organization ID, the cursor sequence, no filters, and `IncludeAssistantEvents = true`. The service-level admin authentication, not the SQL boolean, controls access.
5. Map at most 50 rows to the generated admin response. Build `next_cursor` from the sequence and ID of the 50th returned row when the query over-fetches a 51st row.
6. Return stored actor names directly. Do not call the customer masking helpers or organization-membership lookup.
7. Map `actor_id = "system"` to response display name `System`. Do not backfill or rewrite stored rows.
8. Map null or blank acting surfaces to `unknown`.
9. Preserve snapshots as raw JSON. Return nil metadata for empty or JSON `null` metadata. Decode JSON objects to `map[string]any`. Return `oops.CodeUnexpected` for malformed or non-object metadata.

- [ ] **Step 6: Run the focused admin tests**

Run the command from Step 4.

---

### Task 4: Prove AGE-3342 attribution through the private API

**Files:**

- Modify: `server/internal/admin/listorganizationactivity_test.go`

- [ ] **Step 1: Add an integration test with a real admin write**

Use an authenticated `AdminAuthContext` to run an existing trial extension or re-arm operation. Then call `ListOrganizationActivity` for that organization. Require the response row to contain the stored operator name and `acting_surface = admin`. Require the name not to be `Speakeasy Team`. This proves the AGE-3342 write path and the AGE-3343 private read path together.

- [ ] **Step 2: Run the Activity and existing customer-mask tests**

Run:

```bash
mise run test:server ./internal/admin/ ./internal/auditapi/ -run '^(TestListOrganizationActivity|TestAuditService_List_MasksAdminSurfaceActors|TestAuditService_ListFacets_MasksAdminSurfaceActors)$' -count=1
```

---

### Task 5: Add release notes and verify the complete change

**Files:**

- Create: `.changeset/admin-organization-activity-api.md`

- [ ] **Step 1: Add a server patch changeset**

State that the admin API can list all Activity for an explicit organization with cursor pagination and private actor attribution. Do not describe the customer masking rule as changed; AGE-3342 owns that behavior.

- [ ] **Step 2: Format changed files**

Run:

```bash
hk fix
```

Review the result and revert changes outside this plan.

- [ ] **Step 3: Regenerate and verify generated output**

Run:

```bash
mise run gen:sqlc-server
mise run gen:goa-server
git diff --check
```

Run both generators a second time and confirm they produce no new diff.

- [ ] **Step 4: Run relevant package tests**

Run:

```bash
mise run test:server ./internal/audit/ ./internal/auditapi/ ./internal/admin/
```

- [ ] **Step 5: Run the server build**

Run:

```bash
mise build:server
```

- [ ] **Step 6: Request review**

Use `verification-before-completion`, then `requesting-code-review`. Review must check:

- the endpoint cannot fall back to a session organization;
- an unknown or slug-only target returns not found;
- another organization's rows cannot appear;
- assistant events are included only for the private admin caller;
- private admin names are returned without changing customer masking;
- system actors have a clear label;
- cursor pages have no duplicates or gaps.
