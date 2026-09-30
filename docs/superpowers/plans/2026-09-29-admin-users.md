# Admin Users Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship GRW-214: staff user discovery with tokenized search and organization actions.

**Architecture:** Extend the existing staff Admin service, SQLc reads, React admin UI, and Admin MCP. Parse the same small grammar in Go/TypeScript, checked against one fixture corpus. Page users first, batch-load bounded org previews, and fetch overflow memberships only when requested.

**Tech Stack:** Go/Goa/SQLc/PostgreSQL; React/TanStack Router+Query+Table; existing Radix primitives, Lucide, Vitest.

**Spec:** `docs/superpowers/specs/2026-09-29-admin-users-design.md` — read before executing.

## Global constraints

- Organizations is the visual source of truth; Slack is an interaction reference only. Reuse existing components; no dependencies or unrelated refactors.
- One row per non-deleted local user, including zero-org/no-login accounts. Exclude deleted memberships, not disabled orgs. No trial filter, editing, detail/peek, or bulk workflow.
- Labels: **Last login**, **Not recorded**, **Open in Dashboard**. Existing canonical-ID staff handoff only; no handoff secrets in read results.
- Instant draft/pill UI; **300 ms debounce**, URL **replace-on-edit**, explicit last-valid-result ownership, disabled pagination for invalid/placeholder state.
- Go/API/MCP validate independently. Literal substring semantics; explicit org terms match one org. Parameterized SQL; no production access.
- Generated Go/router/OpenAPI outputs regenerated, never hand-edited. Migrations only if measured need, generated with repository tasks.
- Shared `users` are sensitive/global: fictional fixtures only; use guarded local admin seed, not public demo expansion.
- Skills during execution: `ponytail`, `test-driven-development`, `using-git-worktrees` (verify this existing worktree), `postgresql`, `maintaining-admin-mcp`, `maintaining-platform-mcp`, `gram-demo-seed`, `vercel-react-best-practices`; `pitchfork` before services, `gram-playwright-cli` for browser, `verification-before-completion`, `requesting-code-review`. If making a PR, read repository pull-request guidance and use `pull-request-demo`.

## Contracts selected for implementation

| Item           | Contract                                                                                                                                                                                                                                       |
| -------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Query limits   | 2,048 UTF-8 bytes; 20 terms; 256 Unicode code points per decoded term; reject excess                                                                                                                                                           |
| User page      | `GET /admin/users.list?q=&page=1&limit=50`; limit 1–100; positive page; checked int32-safe offset                                                                                                                                              |
| Ordering       | Users: `lower(email), id`; orgs: `lower(name), slug, id`; no sorting UI in v1                                                                                                                                                                  |
| User result    | `{users, total, page, limit}`; row `{id, display_name, email, last_login?, organizations, organization_count}`                                                                                                                                 |
| Org summary    | `{id, name, slug, disabled_at?}`; **3 previews per user**, plus exact active-membership count                                                                                                                                                  |
| Overflow read  | `GET /admin/users.organizations.list?user_id=&page=1&limit=50`; `{organizations,total,page,limit}`; canonical user ID, same eligibility/order/bounds                                                                                           |
| Overflow UX    | No request if previews contain every org. Otherwise load paginated org options when menu opens; “Load more organizations” preserves focus/loaded items. No eager per-row requests, hidden targets, or cap masquerading as full membership list |
| MCP            | `find_users` (default 10/max 20 users); `list_user_organizations` (default 20/max 100 orgs), sharing service/parser. Second tool only exposes the overflow read; no new product workflow                                                       |
| Public surface | Customer Platform MCP intentionally unchanged: cross-tenant staff discovery is not customer-authorized                                                                                                                                         |

Org overflow is the small implementation elaboration needed to satisfy **both** bounded responses and access to every target. Preserve all existing organization-page behavior.

## MCP decision and security-policy review

- **Outcome / target:** intentionally omit customer Platform MCP changes for the global active-user directory and an exact user's cross-organization memberships. This is not a resource inside the customer's organization or selected project.
- **Actor:** authenticated staff operators, via the standalone Admin dashboard or staff Admin MCP. Neither external customer OAuth principals nor managed-assistant identities gain this capability.
- **Existing-tool comparison:** customer Platform MCP's `list_organization_events` reads events within its authorized organization; it is neither a global user directory nor a cross-tenant membership lookup. Staff Admin MCP's existing organization discovery does not discover users. The PR adds staff `find_users` and `list_user_organizations` using the same admin service and bounded safe projections, rather than expanding customer tools or shipped customer skills.
- **Rationale:** customer membership/project authority cannot authorize cross-tenant discovery. No customer tool contract or workflow changes, so existing customer tools and shipped skills remain unchanged.
- **Success evidence:** `server/internal/admin/listusers_test.go` covers unauthenticated/nonstaff HTTP rejection and data/search behavior; `server/internal/adminmcp/tool_users_test.go` covers exact IDs, output bounds and safe errors; `runtime_test.go` covers live staff revocation, read-scope loss and customer-audience rejection for the new tools. Browser acceptance remains blocked, not passed; performance is deferred to GRW-216.

### Global reads: authorization boundary and unresolved policy exception

The approved product scope is a staff-only **global** directory. Tenant filtering would silently change that scope and hide users without organizations. HTTP endpoints inherit Admin security and run `Service.APIKeyAuth` / `Verifier.Authorize` (or the already-verified admin middleware path). Admin MCP separately authenticates a staff-audience token, performs live staff verification, and requires `admin:read` on invocation. Names, emails and user IDs are selectors, never authorization. Customer Platform MCP does not expose these reads. Existing admin organization discovery is also global; that precedent explains the architecture but does not grant a policy exception.

`cubic.yaml`'s security rule requires SQLc queries to be organization/project scoped, and `REVIEW.md` says every query MUST be project-scoped. Neither text documents an exception for this staff directory. Consequently review comment **4139169937 remains blocked pending explicit human security-policy approval/clarification before shipping**. No exception is asserted, no review safeguard is bypassed, and no tenant predicate has been invented to conceal the conflict. The authorization tests are evidence of the existing boundary, not evidence of policy approval.

## File/dependency map

1. Grammar: `server/internal/admin/usersearch.go`, `usersearch_test.go`, `testdata/user_search.json`; `client/admin/src/lib/userSearch.ts`, `userSearch.test.ts`.
2. API: `server/design/admin/design.go`; `server/internal/admin/users.go`, `queries.sql`, `listusers_test.go`, `listuserorganizations_test.go`; generated `server/gen/` and `server/internal/admin/repo/`.
3. Seed/perf: extend `server/internal/demoseed/{admin.go,admin_test.go,admin_safety_test.go}`; measured evidence in `docs/superpowers/verification/2026-09-29-admin-users.md`.
4. MCP: `server/internal/adminmcp/{runtime.go,tool_users.go,tool_users_test.go,runtime_test.go}`.
5. UI input: `client/admin/src/pages/users/{UserSearchInput.tsx,UserSearchInput.test.tsx}`.
6. UI page: `client/admin/src/pages/users/{index.tsx,index.test.tsx,columns.tsx,UserActions.tsx,UserActions.test.tsx}`; `client/admin/src/routes/{users.tsx,users.index.tsx}`; existing `lib/{gramAdminApi.ts,gramAdminApi.test.ts,adminQueries.ts,adminNav.ts}`; affected navigation/header tests.

Dependency order: **1 → 2 → 3**; **2 → 4**; **1 → 5**; **2+5 → 6 → final verification**. After contracts land, MCP/input/fixture work can be independent. One owner for shared files/generation.

## Task 1 — Search grammar, shared conformance corpus

**Interfaces**

```go
// server/internal/admin/usersearch.go; field is any/name/email/org.
type UserSearchTerm struct { Field string `json:"field"`; Value string `json:"value"` }
func ParseUserSearch(query string) ([]UserSearchTerm, error)
```

```ts
// client/admin/src/lib/userSearch.ts; offsets are UTF-16 positions for selection.
export type UserSearchTerm = {
  field: "any" | "name" | "email" | "org";
  value: string;
  start: number;
  end: number;
};
export type ParsedUserSearch =
  | { ok: true; terms: UserSearchTerm[] }
  | { ok: false; message: string; start: number; end: number };
export function parseUserSearch(query: string): ParsedUserSearch;
export function serializeUserSearch(
  terms: Pick<UserSearchTerm, "field" | "value">[],
): string;
```

- [ ] Add shared JSON cases with `query`, `terms` or `error`; Go reads `testdata/user_search.json`, TS reads the same file via `new URL('../../../../server/internal/admin/testdata/user_search.json', import.meta.url)`. Keep original value case; lowercase prefix only. Compare terms excluding TS offsets.

```json
{
  "query": "name:Alex org:\"Example Studio\"",
  "terms": [
    { "field": "name", "value": "Alex" },
    { "field": "org", "value": "Example Studio" }
  ]
}
```

```go
func TestParseUserSearchBasic(t *testing.T) {
    got, err := ParseUserSearch(`name:Alex org:"Example Studio"`)
    require.NoError(t, err)
    require.Equal(t, []UserSearchTerm{{Field: "name", Value: "Alex"}, {Field: "org", Value: "Example Studio"}}, got)
}
```

- [ ] Cover empty input; mixed/duplicate terms; quoted bare colon; field-case; apostrophe, `%`, `_`, literal/escaped backslash and quote; Unicode/emoji offsets and byte/code-point bounds; unknown prefix, empty value, unclosed quote, adjacent malformed quoted token. Explicit `OR`, grouping, and negation syntax get an unsupported-syntax error; quote these when literal text is intended.
- [ ] Run red: `mise run test:server ./internal/admin -run TestParseUserSearch -count=1`; `aube run -F admin test src/lib/userSearch.test.ts`.
- [ ] Implement small linear scanners and canonical serializer, not a general query language. Token spans retain editable source; serializer quotes bare terms containing colon/whitespace/syntax and escapes quotes/backslashes. Shared fixtures assert error parity and `parse(serialize(terms))` equality.
- [ ] Run green with the same commands. Review/commit only grammar and fixtures (`feat: define admin user search grammar`).

## Task 2 — Staff list and membership reads

**Interfaces:** Goa `Service.ListUsers(ctx,*gen.ListUsersPayload) (*gen.AdminListUsersResult,error)` and `Service.ListUserOrganizations(ctx,*gen.ListUserOrganizationsPayload) (*gen.AdminListUserOrganizationsResult,error)`. Fields match the contract table. No generated files edited directly.

- [ ] Add Goa types/methods beside existing member/list contracts. Reuse service-level staff auth; add HTTP authorization assertions patterned on `listorganizationactivity_test.go`. Generate: `mise run gen:goa-server`.
- [ ] Write failing `TestListUsers`/`TestListUserOrganizations` cases using existing admin setup/testrepo helpers. Fixtures: one zero-org user, two same-name users, missing login/name, deleted user, WorkOS-deleted user, removed membership, disabled org, one user with distinct Studio and North orgs, duplicate-name orgs, one user with >3 orgs.

```text
q=org:studio org:north  → excludes user whose two different orgs match separately
q=org:studio north      → includes that user
q=email:example.invalid → ignores deleted accounts; zero-org eligible account remains
q=name:"100%_Literal"   → %/_ match literally, not SQL wildcards
page beyond last       → empty users, correct total; no duplicate users across tied-email pages
```

- [ ] Run red: `mise run test:server ./internal/admin -run 'TestList(Users|UserOrganizations)' -count=1`.
- [ ] Implement in `users.go`; keep `impl.go` free of another large handler block. Existing lifecycle SQL clears both deletion markers on reactivation: require both null. Convert parsed terms into escaped pattern arrays; empty arrays are non-null. Escape backslash, then `%`, then `_`. Go errors become safe invalid-input responses, not SQL details.
- [ ] Add SQLc reads `AdminListUsers`, `AdminCountUsers`, `AdminListUsersOrganizationPreviews`, `AdminListUserOrganizations`, `AdminCountUserOrganizations`. Page/count predicates identical; page from `users`, not a membership join. AND names/emails/bare terms; group explicit org patterns inside one membership `EXISTS`:

```sql
EXISTS (
  SELECT 1 FROM organization_user_relationships m
  JOIN organization_metadata o ON o.id = m.organization_id
  WHERE m.user_id = u.id AND m.deleted IS FALSE
    AND NOT EXISTS (
      SELECT 1 FROM unnest(@org_patterns::text[]) AS p(pattern)
      WHERE NOT (o.name ILIKE p.pattern OR o.slug ILIKE p.pattern)
    )
)
```

Skip that EXISTS when the org-pattern array is empty. Each bare term has its own name/email/org OR condition. Previews: one batch query over page user IDs, membership counts plus top 3 orgs per user, no per-user round trips. Overflow read validates the canonical eligible user, returning not-found for deleted/missing accounts.

- [ ] Generate SQLc with local infrastructure (`mise run infra:start`, then `mise run gen:sqlc-server`); run green. Verify header/session/nonstaff rejection on both HTTP routes and literal/bounds tests. Follow established OpenAPI/SDK generation when required; don't hand-edit generated output or rebuild every SDK without checking task scope.
- [ ] Review/commit API + generated changes (`feat: add staff user search reads`).

## Task 3 — Local fixtures and measured query plans

**Consumes:** Task 2 SQL/eligibility. **Produces:** idempotent local seed coverage and recorded page/count plans; indexes only with evidence.

- [ ] Extend `TestAdminSeedFixtures` and `TestAdminSeedSafetyAndIdempotency` before changing seed. Assert fictional user/membership edge cases above, more than one results page, >3 memberships, unchanged unrelated sentinel rows after two runs.
- [ ] Run red: `mise run test:server ./internal/demoseed -run TestAdminSeed -count=1`; safety check: `mise run test:server -tags=demoseed_safety ./internal/demoseed -run TestAdminSeedSafetyAndIdempotency -count=1`.
- [ ] Extend **existing** `server/internal/demoseed/admin.go` guarded local fixtures, retaining `environment=local`, development database/user, and loopback guards. Do not modify public demo SQL or create a parallel fixture system. Run `mise run seed:admin` twice; rerun both tests.
- [ ] Measure exact generated page/count SQL, not a simplified stand-in, using existing local test DB support. Synthetic profiles: 10k users/2k orgs and 100k users/20k orgs; normally 0–3 memberships plus a 200-org user. No performance fixture is enabled by default in the demo. Seed only an isolated local test DB/transaction; never wipe shared data.

Use `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) EXECUTE` against prepared statements containing the **Task 2 SQL**, with concrete escaped-pattern arrays, limit, and offset. Save those exact reproducible statements/parameters in the verification document; don't substitute an isolated `ILIKE` microbenchmark.

Matrix: empty, selective email, selective org, common bare term, no match, one/two characters, mixed/repeated orgs; first/deep pages; previews and overflow. Record environment, exact parameters/SQL, cardinalities, three-run timing median, buffers, row estimates and total request cost. Provisional target: page+count+previews ≤300 ms at 100k synthetic users; report deviations, never equate local results with production assurance.

- [ ] If target fails, inspect actual plans first. Evaluate `pg_trgm` GIN indexes and index-friendly candidate-ID query shapes only for measured bottlenecks; remeasure short/no-match/common terms and write cost. Schema changes require `postgresql` skill, `mise run db:diff admin_user_search_indexes`, generated migration review, `mise run lint:migrations`, and re-running relevant tests. If no change needed, explicitly record that result.
- [ ] Save compact reproducible evidence in `docs/superpowers/verification/2026-09-29-admin-users.md`; review/commit fixture/perf changes. Infrastructure unavailable: record blocker, don't claim efficiency.

## Task 4 — Staff Admin MCP parity

**Files:** new `tool_users.go`, `tool_users_test.go`; existing `runtime.go`, `runtime_test.go`.

```go
// Follow existing optional reader capability registration.
type UserReader interface {
    ListUsers(context.Context, *gen.ListUsersPayload) (*gen.AdminListUsersResult, error)
    ListUserOrganizations(context.Context, *gen.ListUserOrganizationsPayload) (*gen.AdminListUserOrganizationsResult, error)
}
func registerUserTools(server *mcp.Server, reads UserReader)
```

- [ ] Write failing tool tests with `callStaffReadTool`: `find_users` query/page/limit defaults and bounds; `list_user_organizations` canonical ID/pagination; unavailable reader; malformed/oversized grammar; zero-org result; preview count/overflow; no handoff/provider secrets. Extend runtime tests for read scope and revoked/nonstaff/customer credentials. Both tools read-only.
- [ ] Run red: `mise run test:server ./internal/adminmcp -run 'Test.*(Users|UserOrganizations|Runtime)' -count=1`.
- [ ] Register `UserReader` via the existing optional reader type-assertion pattern, not a new factory. Tools call the service, never duplicate SQL/parser or accept session keys. Preserve safe validation messages; use generic operational errors. Tool descriptions document prefixes/AND/same-org and bounded previews; instructions treat names as untrusted data and direct overflow reads to exact user IDs.
- [ ] Run green plus existing organization-tool/authenticator tests. Record decision: staff tools added; customer Platform MCP unchanged because this is cross-tenant staff access. Review/commit (`feat: expose staff user lookup tools`).

## Task 5 — Accessible token search input

**Files:** new `pages/users/UserSearchInput.tsx` and test. Reuse Task 1 parser/serializer; existing `Badge`, `Button`, `Input` styling and Lucide.

```ts
export type UserSearchInputProps = {
  value: string; // Full draft query, including an in-progress term.
  onChange: (value: string) => void; // Synchronous; page owns the 300ms debounce.
  error?: string;
};
export function UserSearchInput(props: UserSearchInputProps): React.JSX.Element;
```

- [ ] Write failing component tests: Space/Enter vs quoted-space; keyboard/click edit; Escape restores pre-edit term; named × controls; two-step Backspace; focus return; paste without delimiter; composition; undo of pill edit/removal; external value changes; incomplete and unsupported syntax help. Example assertion:

```tsx
const onChange = vi.fn();
render(<UserSearchInput value="email:example.invalid" onChange={onChange} />);
expect(
  screen.getByRole("button", { name: "Edit email filter: example.invalid" }),
).toBeVisible();
```

- [ ] Run red: `aube run -F admin test src/pages/users/UserSearchInput.test.tsx`.
- [ ] Implement a normal text input plus named edit/remove buttons inside secondary badges; no contenteditable or new component framework. Keep draft order/spans for editing; external committed values tokenize without trailing whitespace. Pill commits must not change query meaning. Use a local editor-state history for token transformations and ensure Ctrl/Cmd+Z restores them without fighting native text undo; reset that history on external navigation. No request/debounce logic inside this component.
- [ ] Test screen-reader labels/status, focus after removal, IME/paste/undo in a real browser during final verification; unit tests alone do not prove native input behavior. Run green; review/commit (`feat: add tokenized admin user search input`).

## Task 6 — Users page, navigation, and organization actions

**Files:** map above. **Consumes:** Task 2 APIs, Task 5 component. **Produces:** `/users` with URL `{q?:string,page?:number}` and query-keyed results.

```ts
// lib/gramAdminApi.ts; nullable timestamps are omitted when absent.
export type AdminUserOrganization = {
  id: string;
  name: string;
  slug: string;
  disabled_at?: string;
};
export type AdminUser = {
  id: string;
  display_name: string;
  email: string;
  last_login?: string;
  organizations: AdminUserOrganization[];
  organization_count: number;
};
export type AdminListUsersResult = {
  users: AdminUser[];
  total: number;
  page: number;
  limit: number;
};
export type AdminListUserOrganizationsResult = {
  organizations: AdminUserOrganization[];
  total: number;
  page: number;
  limit: number;
};
export function listUsers(
  params: { q?: string; page?: number; limit?: number },
  signal?: AbortSignal,
): Promise<AdminListUsersResult>;
export function listUserOrganizations(
  params: { user_id: string; page?: number; limit?: number },
  signal?: AbortSignal,
): Promise<AdminListUserOrganizationsResult>;
// lib/adminQueries.ts: queryOptions with complete params in each key;
// queryFn forwards its AbortSignal to the helpers above.
```

- [ ] Add client contract/encoding tests in `gramAdminApi.test.ts`, then page/action tests. Mock both endpoints through existing admin router/query test helpers. Fake-timer expectations: no request at 299 ms, exactly one final query at 300 ms; canceled old query can't replace current results; no new request for invalid draft; old results labeled and pager disabled; Back/Forward cancels timer/restores pills; clear resets query/page; route errors never silently widen query.
- [ ] Run red: `aube run -F admin test src/lib/gramAdminApi.test.ts src/pages/users/index.test.tsx src/pages/users/UserActions.test.tsx`.
- [ ] Add client/query helpers and route search validation. Separate draft and URL-committed state; query results carry their originating key. Use `replace: true` as in Organizations Toolbar; valid commits reset page. Retain explicit last-valid data when draft invalid, without treating that data as a cache hit for the draft. No query enabled for invalid direct URLs. Prefer complete query keys plus abort signals over custom request-version infrastructure.
- [ ] Build page/columns from Organizations table and pagination conventions. Test missing values and exact login timestamp; use Users icon in `ADMIN_NAV_GROUPS`, static breadcrumb in `routes/users.tsx`, index route in `users.index.tsx`. Generate router types through existing build/plugin workflow. Update existing navigation/header/palette expectations rather than duplicating navigation.
- [ ] Implement `UserActions` with shared Radix submenu primitives, email-fallback accessible labels, existing clipboard/toast patterns. View org by canonical ID (do not seed a full-org query cache from a summary). Dashboard uses `openOrganizationDashboard(org.id)` from `gramAdminApi.ts`, not a fabricated URL or `useOpenOrganization` (that hook expects a full record). Disable dashboard action for disabled orgs, not View Organization.
- [ ] Overflow only when opening a multi-org menu whose count exceeds previews: query sorted membership pages, dedupe by org ID, show count/Load more, loading/error retry, preserve keyboard focus. If counts change, continue based on endpoint pagination, not stale preview assumptions. Test 0/1/many/>page memberships, duplicate names, failed overflow, exact canonical targeting, and no requests merely rendering rows.
- [ ] Run green and `aube run -F admin type-check`; review/commit (`feat: add admin users directory`).

## Final verification / handoff

- [ ] Run focused parser/API/MCP tests, full admin UI tests, then server build/lint according to available infrastructure. Commands:

```bash
mise run test:server ./internal/admin ./internal/adminmcp
mise run test:server -tags=demoseed_safety ./internal/demoseed -run TestAdminSeed
mise run build:server
mise run lint:server
aube run -F admin test
aube run -F admin type-check
aube run -F admin build
git diff --check
```

- [ ] Use `gram-playwright-cli` and `mise run playwright` against local Admin. Compare Organizations/Users side-by-side; verify narrow table/sticky Actions, Lucide icons, badge/input/focus styling, valid/invalid/debounced searches, URL restoration, clipboard, IME/paste/undo, nested menu keyboard and overflow, disabled targets and real local support handoff. Capture only fictional data. Record evidence/limitations alongside query measurements.
- [ ] Self-check every spec section against tests; request independent correctness/security and UX review. Fix findings, rerun affected checks. Confirm generated outputs and customer confidentiality before any commit/push/PR; no bypasses. No claim of passing checks without actual outputs.

**Unresolved questions:** none. Index choice depends on Task 3 measurements; unavailable infrastructure is a verification blocker, not a design question.
