# AGE-3344 Organization Activity Page Implementation Plan

> **For the implementer:** Use `using-superpowers`, `test-driven-development`, `vercel-react-best-practices`, `gram-demo-seed`, `gram-playwright-cli`, `pr-demo-gif`, `verification-before-completion`, and `requesting-code-review`. Use `pull-request` only when you prepare the PR.

**Goal:** Add an Activity view to the admin organization record. Show every organization audit event from AGE-3343 in server order. Keep the organization record context and use the resolved organization ID for every activity request.

**Architecture:** Keep the route parameter as the record address. Resolve it with the existing `organizationQuery(idOrSlug)`. Pass `org.id` to one cursor-based infinite query. Flatten pages in response order. Render a compact event list with native disclosure elements for raw details. Do not add search, filters, facets, export, grouping, or client-side sorting.

**Stack:** React 19, TanStack Router, TanStack Query, Vitest, Testing Library, Tailwind, and the hand-written admin API client.

**Base:** This branch must remain stacked on AGE-3343 commit `240c67190`. That commit supplies `GET /admin/organization.activity`. AGE-3150 follows AGE-3344 and also depends on AGE-3131; do not pull its trial-specific presentation or evidence into this plan.

## Fixed decisions

- Add `/organizations/:idOrSlug/activity`.
- Preserve `idOrSlug` in the address and in record navigation links.
- Resolve `idOrSlug` through `organizationQuery`. Call activity with `org.id`. Never send the slug as `organization_id`.
- Use the AGE-3343 response without a new server change.
- Render every returned action as its raw action string. Keep mixed event classes, including trial and non-trial actions, in one list; do not add event-specific labels, descriptions, badges, or detail layouts.
- Preserve API page order and item order. Do not sort, group, filter, or deduplicate events in the client. The AGE-3343 cursor contract prevents page overlap and omissions.
- Use a `Load more` control. Keep all loaded events on screen.
- Use one infinite-query cache entry per explicit organization ID. Use `next_cursor` as the next `pageParam`. Stop when `next_cursor` is absent.
- Show the event time, action, actor, subject, and acting surface in the event summary.
- Show project, actor type and ID, actor slug, subject type and ID, subject slug, acting client ID, before snapshot, after snapshot, and metadata in the disclosure when each value exists.
- Prefer `actor_display_name`. Use `System` for the system actor. Then fall back to actor slug and actor ID. This keeps private staff names from AGE-3343 visible to operators.
- Prefer `subject_display_name`. Then fall back to subject slug and subject ID.
- Use `<time dateTime={created_at}>`. Format a full UTC date and time with a page-local `Intl.DateTimeFormat`. Do not change the date-only shared helper.
- Use native `<details>` and `<summary>`. Use wrapped monospace `<pre>` blocks for snapshots and metadata. Use `JSON.stringify(value, null, 2)`. Do not add a JSON-viewer abstraction or dependency.
- Distinguish five states: initial loading, empty, initial error, later-page loading, and later-page error. Keep loaded rows visible during all later-page states.
- Keep the existing organization record header, callouts, actions, breadcrumb, and sidebar unchanged.
- AGE-3150 owns trial-specific labels and details after AGE-3344 and AGE-3131. AGE-3344 owns only generic raw actions plus useful raw metadata and snapshots.
- Add no search, filters, facets, export, URL search state, or new server endpoint.

## Task 1: Add the hand-written activity API contract

**Files:**

- Modify: `client/admin/src/lib/gramAdminApi.test.ts`
- Modify: `client/admin/src/lib/gramAdminApi.ts`

### 1. Write the failing API tests

Add a `listOrganizationActivity` test group. Stub `fetch` with the existing local pattern. Cover these cases:

1. `listOrganizationActivity("org explicit")` calls `/admin/organization.activity?organization_id=org+explicit`.
2. The first request omits `cursor`. It must not send `cursor=`.
3. `listOrganizationActivity("org explicit", "next cursor")` appends `&cursor=next+cursor`.
4. The function returns `logs` and `next_cursor` without changing their order or values.

Use fictional values only. Include one response with a user actor, one system actor, snapshots, metadata, a project, and an acting client.

Define the client types from the AGE-3343 wire contract:

```ts
export type AdminAuditLog = {
  id: string;
  project_id?: string;
  project_slug?: string;
  actor_id: string;
  actor_type: string;
  actor_display_name?: string;
  actor_slug?: string;
  action: string;
  acting_surface: string;
  acting_client_id?: string;
  subject_id: string;
  subject_type: string;
  subject_display_name?: string;
  subject_slug?: string;
  before_snapshot?: unknown;
  after_snapshot?: unknown;
  metadata?: Record<string, unknown>;
  created_at: string;
};

export type ListOrganizationActivityResult = {
  logs: AdminAuditLog[];
  next_cursor?: string;
};
```

### 2. Run the red check

```bash
aube run -F admin test src/lib/gramAdminApi.test.ts
```

Expected: FAIL because `listOrganizationActivity` and its types do not exist.

### 3. Add the smallest API function

Add `listOrganizationActivity(organizationID, cursor?)` beside the other organization list functions. Use `toSearchParams({ organization_id: organizationID, cursor })`. Call `gramAdminFetch<ListOrganizationActivityResult>`. Do not accept `idOrSlug` as the parameter name.

### 4. Run the green check

```bash
aube run -F admin test src/lib/gramAdminApi.test.ts
```

Expected: PASS.

## Task 2: Add one organization-scoped infinite query

**Files:**

- Modify: `client/admin/src/lib/adminQueries.test.ts`
- Modify: `client/admin/src/lib/adminQueries.ts`
- Modify: `client/admin/src/test/fixtures.ts`

### 1. Write the failing query tests

Add tests for `organizationActivityQuery(organizationID)`. Verify these rules:

1. The query key contains the explicit organization ID. Two organization IDs produce different keys.
2. The initial page calls `listOrganizationActivity(organizationID, undefined)`.
3. A later page passes the exact opaque cursor to `listOrganizationActivity`.
4. `getNextPageParam` returns `next_cursor`. It returns `undefined` when the cursor is absent.

Do not put each cursor in a separate query key. One infinite query owns all pages for one organization.

### 2. Run the red check

```bash
aube run -F admin test src/lib/adminQueries.test.ts
```

Expected: FAIL because the activity query factory does not exist.

### 3. Add the query factory and fixture

Import `infiniteQueryOptions`. Add `organizationActivityQuery(organizationID)`. Set `initialPageParam` to `undefined`. Forward `pageParam` as the cursor. Return `lastPage.next_cursor` from `getNextPageParam`.

Add `anActivityLog(overrides)` to `client/admin/src/test/fixtures.ts`. Use invented actor, subject, project, and action values. Keep the fixture neutral so each test controls the field under test.

Do not copy query data into component state. Do not use an effect to join pages. TanStack Query must own the page state.

### 4. Run the green check

```bash
aube run -F admin test src/lib/adminQueries.test.ts
```

Expected: PASS.

## Task 3: Build the Activity view with state-specific tests

**Files:**

- Create: `client/admin/src/pages/organization/Activity.test.tsx`
- Create: `client/admin/src/pages/organization/Activity.tsx`

### 1. Write failing presentation tests

Use the existing Vitest, Testing Library, `vi.hoisted`, partial API mock, and `renderWithApp` patterns. Keep React Query retries disabled. Test the leaf `Activity` component with an explicit organization record before route integration.

Cover the populated view first:

1. The API mock rejects every argument except `org.id`. This proves the leaf never sends `org.slug`.
2. Render interleaved canonical actions such as `organization:enterprise_trial_extended`, `organization:webhooks_enabled`, and `organization:enterprise_trial_demoted`. Assert their raw strings and DOM order match the response. Assert there is one list. Do not add action-specific presentation.
3. Assert the visible summary shows full UTC time, action, actor, subject, and acting surface.
4. Assert a staff event uses `actor_display_name`.
5. Assert a system event reads `System`.
6. Assert actor and subject fallbacks use slug, then ID, when display names are absent.
7. Expand an event. Assert project details, actor details, subject details, acting client ID, before snapshot, after snapshot, and metadata.
8. Assert absent optional values do not create fake `null`, `{}`, or dash-only detail sections.
9. Assert JSON values remain structured and readable. Do not convert booleans or numbers to labels.

### 2. Write failing initial-state tests

Cover these states with separate tests:

- A never-resolving first request shows `Loading activity...`. It does not show the empty or error text.
- A resolved first page with `logs: []` and no cursor shows `No activity for this organization`.
- A rejected first request shows `Unable to load activity` and a `Retry` button. It does not show the empty text.
- The retry control runs the first-page query again.

Use `isPending`, not `isLoading`, for the initial branch. A paused first request is still pending.

### 3. Write failing cursor-boundary tests

Return page one with IDs `event-4`, `event-3` and `next_cursor: "cursor-3"`. Keep page two deferred. Click `Load more`. Verify all of these points:

1. The second API call uses `org.id` and `cursor-3`.
2. Page-one events stay visible.
3. The control reads `Loading...` and is disabled.
4. A second click cannot start another request.

Resolve page two with IDs `event-2`, `event-1` and no cursor. Assert the exact order `event-4`, `event-3`, `event-2`, `event-1`. Assert each ID appears once. Assert `Load more` disappears. This is the client boundary check for no duplicate or omitted events.

Do not add a client deduplication layer. A deduper can hide a broken cursor contract and cannot repair an omission.

### 4. Write the failing later-page error test

Reject page two after page one succeeds. Verify these points:

1. Page-one events stay visible.
2. The page shows `Unable to load more activity`.
3. The page does not show the initial error state.
4. A `Retry loading more` control calls `fetchNextPage` with the same cursor.
5. A successful retry appends page two once and clears the error.

### 5. Run the red check

```bash
aube run -F admin test src/pages/organization/Activity.test.tsx
```

Expected: FAIL because the page does not exist.

### 6. Add the smallest view

Implement `Activity({ org })` with `useInfiniteQuery(organizationActivityQuery(org.id))`. Flatten `data.pages` with `flatMap` in render order. Do not add derived state or an effect.

Use semantic elements:

- A page heading named `Activity`.
- One `<ol>` for all events.
- One `<li>` per event, keyed by event ID.
- A `<time>` with the server timestamp in `dateTime`.
- A native `<details>` disclosure named for the event.
- A button for initial retry.
- A separate button for later-page retry.
- A `Load more` button only when `hasNextPage` is true.

Use the existing button and muted-text styles. Keep the layout compact. Do not add a generic timeline, JSON viewer, activity card, or pagination component.

### 7. Run the green check

```bash
aube run -F admin test src/pages/organization/Activity.test.tsx
```

Expected: PASS.

## Task 4: Add the route and organization record navigation

**Files:**

- Create: `client/admin/src/routes/organizations.$idOrSlug.activity.tsx`
- Modify: `client/admin/src/pages/organization/Activity.tsx`
- Modify: `client/admin/src/pages/organization/Activity.test.tsx`
- Modify: `client/admin/src/components/record-nav.test.tsx`
- Modify: `client/admin/src/components/record-nav.tsx`
- Modify: `client/admin/src/components/app-sidebar.test.tsx`
- Modify: `client/admin/src/components/site-header.test.tsx`
- Modify: `client/admin/src/pages/organization/RecordLayout.test.tsx`
- Regenerate: `client/admin/src/routeTree.gen.ts`

### 1. Write the failing route test

Add `ActivityRoute` beside `Activity`. Follow `ProjectsRoute` and `MembersRoute`:

1. Read `idOrSlug` from `/organizations/$idOrSlug`.
2. Read `organizationQuery(idOrSlug)`.
3. Return `null` while the parent owns initial organization loading and error UI.
4. Pass the resolved record to `<Activity org={data} />`.
5. Set `staticData: { crumb: "Activity" }` on the file route.

Before implementation, add a route-tree test with `initialPath: /organizations/test-org/activity`. Mock `getOrganization("test-org")` to return `{ id: "org_1", slug: "test-org" }`. Make the activity mock reject `test-org` and accept only `org_1`. Assert the Activity heading and event appear.

This test proves that the address can use a slug while AGE-3343 receives an explicit ID.

### 2. Write the failing navigation tests

Extend `record-nav.test.tsx` and `app-sidebar.test.tsx`. Verify these rules:

- `Activity` is present with the other organization record items.
- Its destination is `/organizations/<current-idOrSlug>/activity`.
- A record reached by ID stays addressed by ID.
- A record reached by slug stays addressed by slug.
- Extend the `RECORD_NAV` expectation in `app-sidebar.test.tsx` with `Activity` in the implemented order.
- For both ID and slug activity routes, Activity is the sole item with the active style and `aria-current="page"`; Overview and every other record item are inactive and not current.

Extend `site-header.test.tsx` with the activity route and assert the breadcrumb reads `Organizations / <organization> / Activity`. This depends on the route's `staticData` crumb rather than pathname parsing.

Use a small existing Lucide icon, such as `History`. Do not add an icon component.

### 3. Write the failing record-context test

Extend `RecordLayout.test.tsx` with the activity child route. Assert that the organization name, record header, existing record context, record actions, and Activity content render together. This prevents the new page from replacing the record layout and does not add trial-specific presentation.

### 4. Run the red checks

```bash
aube run -F admin test \
  src/pages/organization/Activity.test.tsx \
  src/components/record-nav.test.tsx \
  src/components/app-sidebar.test.tsx \
  src/components/site-header.test.tsx \
  src/pages/organization/RecordLayout.test.tsx
```

Expected: FAIL because the route and navigation item do not exist.

### 5. Add the route and navigation item

Create the file route for `/organizations/$idOrSlug/activity`. Point it at `ActivityRoute` and add `staticData: { crumb: "Activity" }`. Add an exact activity match to `RecordNav`. Pass the current `idOrSlug` to the link. Do not replace it with `org.slug`.

### 6. Generate the route tree

Run the repository build command. The TanStack Router Vite plugin reads `src/routes` and writes `src/routeTree.gen.ts`. Do not edit the generated file by hand.

```bash
aube run -F admin build
```

Expected: PASS. The generated tree contains `/organizations/$idOrSlug/activity`.

### 7. Run the green checks

```bash
aube run -F admin test \
  src/pages/organization/Activity.test.tsx \
  src/components/record-nav.test.tsx \
  src/components/app-sidebar.test.tsx \
  src/components/site-header.test.tsx \
  src/pages/organization/RecordLayout.test.tsx
```

Expected: PASS.

## Task 5: Add release notes and run focused verification

**Files:**

- Create: `.changeset/admin-organization-activity-page.md`
- Verify only: `client/admin/**`

### 1. Add the admin patch changeset

Use this frontmatter:

```md
---
"admin": patch
---
```

State that the organization record now has an Activity view. State that it shows all audit events, actor and subject details, snapshots, metadata, surface, and cursor-loaded history. Do not mention customer data.

### 2. Run the focused test set

```bash
aube run -F admin test \
  src/lib/gramAdminApi.test.ts \
  src/lib/adminQueries.test.ts \
  src/pages/organization/Activity.test.tsx \
  src/components/record-nav.test.tsx \
  src/components/app-sidebar.test.tsx \
  src/components/site-header.test.tsx \
  src/pages/organization/RecordLayout.test.tsx
```

Expected: PASS.

### 3. Run all admin tests

```bash
aube run -F admin test
```

Expected: PASS.

### 4. Run the focused static checks

```bash
aube run -F admin type-check
aube run -F admin lint:oxlint
aube run -F admin build
```

Expected: PASS. The final build also proves that route generation and code splitting succeed.

Run `aube run -F admin lint` only if the local package has the required `oxfmt` binary. Do not treat the known package-local formatter setup issue as an Activity defect.

### 5. Run the stacked API regression check

```bash
mise run test:server ./internal/admin/ -run '^TestListOrganizationActivity' -count=1
```

Expected: PASS. This confirms that AGE-3343 still rejects slugs, returns all event classes in sequence order, and keeps its cursor boundary.

## Task 6: Confirm the no-demo-seed decision

**Files:**

- Do not modify `server/internal/demoseed/**` or `seed/demo/**`.

### Decision

Do not change the demo seed for AGE-3344. The page adds no data model, and the admin app is operator-only. AGE-3344 does not need trial-specific seed rows or trial evidence. AGE-3150 owns trial-specific labels and details after AGE-3344 and AGE-3131.

Use mixed fictional trial and non-trial rows in component tests only to prove that this generic list preserves raw actions, metadata, snapshots, and API order. If local visual verification has insufficient rows, use ignored, transient local-only data; do not turn that evidence setup into a committed demo-seed change.

## Task 7: Verify the UI with Playwright and prepare PR evidence

**Files:**

- Create ignored artifacts only under `.playwright-cli/pr-demos/`
- Do not commit screenshots, video, GIFs, local IDs, or local URLs.

### 1. Start only the required local services

Use the `pitchfork` skill during implementation. Check before starting or restarting anything.

```bash
pitchfork status admin
pitchfork status admin-dashboard
pitchfork start admin
pitchfork start admin-dashboard
mise run seed
mise run zero:summary
```

Read the `Gram admin dashboard` URL from the summary. Do not assume a port when local overrides exist.

### 2. Run the Playwright acceptance pass

Use a named session and the repository wrapper only:

```bash
mise run playwright -s=admin-activity open "<gram-admin-dashboard-url>"
mise run playwright -s=admin-activity goto "<gram-admin-dashboard-url>/organizations/<LOCAL_ORG_ID_OR_SLUG>/activity"
mise run playwright -s=admin-activity snapshot
```

Verify these points in the browser:

- The organization record header, breadcrumb, sidebar, and actions remain visible.
- Activity is current in the record navigation.
- The populated list uses API order.
- Mixed generic events, including canonical `organization:enterprise_trial_extended` and `organization:enterprise_trial_demoted` rows when present, share one list and display raw action strings without event-specific presentation.
- Staff display names and `System` actors render through the generic actor fallback rules.
- Actor, time, raw action, subject, and acting surface are visible.
- Expanding one event shows useful raw identifiers, metadata, and snapshots.
- `Load more` keeps prior rows visible and appends the next page once.
- The page has no search, filters, facets, export, or trial-specific labels/details.
- The browser console has no errors.
- The network request sends `organization_id=<explicit-id>`, even when the route uses a slug.

Automated tests remain the evidence for initial loading, empty, initial error, later loading, later error, and the exact cursor boundary. Visual evidence is limited to mixed generic events, staff and System actors, raw details, and pagination. Do not mutate the shared demo seed or add production controls to make those states easy to capture.

Close the session:

```bash
mise run playwright -s=admin-activity close
```

### 3. Capture focused PR evidence

Use fictional, ignored local-only data with enough rows to expose `Load more`; do not change the demo seed. Capture only:

1. A PNG showing interleaved generic event classes, at least one staff actor, one `System` actor, and one open disclosure with raw metadata or snapshots. Raw canonical trial actions may appear, but no trial-specific label or detail treatment may appear.
2. A short GIF showing `Load more`, prior rows remaining visible, and the next page appended once.

```bash
mkdir -p .playwright-cli/pr-demos
mise run playwright -s=pr-demo open "<gram-admin-dashboard-url>"
mise run playwright -s=pr-demo goto "<gram-admin-dashboard-url>/organizations/<LOCAL_ORG_ID_OR_SLUG>/activity"
mise run playwright -s=pr-demo snapshot
# Open one representative event by its current snapshot ref.
mise run playwright -s=pr-demo click <details-ref>
mise run playwright -s=pr-demo screenshot --hires \
  --filename=.playwright-cli/pr-demos/organization-activity.png
# Record the focused Load more interaction, then convert it with ./tools/ffmpeg.
mise run playwright -s=pr-demo close
```

Follow `pr-demo-gif` for recording and conversion. Inspect both artifacts before publication. Confirm they contain no real customer, organization, project, staff email, ID, or activity data and prove only mixed generic events, staff/System actors, raw details, and pagination.

Publish the approved PNG and GIF through the secret-gist workflow in `pr-demo-gif`. Add a short `### Demo` PR comment. Never pass a binary directly to `gh gist create`.

## Final verification and review

Before claiming completion:

```bash
git status --short
git diff --check
git diff --stat 240c67190...HEAD
```

Confirm that the diff contains only the admin Activity implementation, generated route update, tests, changeset, and this plan. Confirm that no demo seed file changed. Confirm that no customer-identifying data appears in code, fixtures, artifacts, branch text, changeset text, or PR text.

Request code review after all checks pass. Ask the reviewer to focus on explicit-ID use, cursor boundaries, state separation, API-order preservation, organization-context preservation, accessibility, and scope control.

## Unresolved questions

None. The acceptance criteria and repository conventions decide the implementation.
