# GRW-214: Admin Users — agreed design

Reviewed in three independent read-only passes (UX, search/database, staff/API); recommendations accepted. This document preserves the conversational agreement; the HTML mockup is illustrative, not production code.

## Outcome and scope

Staff can find a person by name/email/organization, copy their identity, inspect an organization, and open its dashboard. All non-deleted **locally persisted** users, not trial-only; no live identity-provider directory or pending-invitation import. One row per user, including zero-org users. Active membership means non-deleted, not recent usage. Keep disabled organizations visible; do not infer user inactivity from missing login/membership.

Exclude trial filters, user editing/detail pages, bulk actions, record peek, and a new visual theme.

## UI and actions

- Add Users navigation, command-palette destination, and breadcrumb.
- Columns: Name, Email, Organizations, **Last login**, Actions. Missing name: dash and disabled Copy Name. Missing login: **Not recorded**. Last login is recorded authentication, not product activity; exact time/timezone available with existing date formatting.
- Organizations page is visual source of truth: shared DataTable, Input styling, Button, secondary Badge, DropdownMenu, pagination, Lucide iconography, typography/spacing/borders/states. Sticky Actions column; ghost ellipsis. Slack is the token-interaction reference only.
- Organization summary: first name plus remaining count. Stable organization order; disambiguate duplicate names by slug; keep long targets identifiable.
- Copy Name; Copy Email; View Organization; **Open in Dashboard** with external-link icon. Each organization has its own submenu when several exist. Single-org actions direct; zero-org users get copies only.
- View Organization targets the admin record. Dashboard action reuses existing staff-authenticated handoff by canonical organization ID in a new tab. Disabled organization: dashboard action unavailable. Never manufacture dashboard URLs or return handoff credentials in lists, MCP, or shared URLs.

## Search grammar

- Server-side, paginated, case-insensitive literal substring matching.
- Prefixes: `name:`, `email:`, `org:` (org name or slug); case-insensitive field names. Bare terms match name OR email OR organization. Terms combine with AND.
- Quotes preserve phrases: `org:"Example Studio"`. Quoted bare `"team:foo"` is literal text. Inside quotes, `\"` and `\\` encode quote and backslash; preserve other backslashes literally. Apostrophes, `%`, and `_` are ordinary characters. Escape LIKE patterns after parsing; parameterize SQL.
- All explicit `org:` terms must match **one** active associated organization. Bare terms match independently: `org:studio north` can match different orgs, whereas `org:studio org:north` cannot. Always show all active associated orgs, not only search matches.
- No OR operator, negation, regex, or parentheses. Unknown unquoted prefixes, incomplete filters, and unclosed quotes produce helpful validation hints, not broadened searches. Validate server-side and in MCP, not just in the browser.
- Canonical plain-text query round-trips through URLs. Enforce finite query, term, page, and nested-result limits; reject oversized input rather than silently truncating.

## Token input and request state

- Complete prefixed term becomes a pill on Space/Enter. Quoted values wait for closing quote. Plain text stays text. Click/keyboard-activate pill to edit; Escape cancels edit. × removes; empty-input Backspace selects final pill, next Backspace removes.
- Ignore commit/delete shortcuts during IME composition. Paste preserves query meaning without an extra space. Undo restores removed/edited terms. Named keyboard-accessible controls, predictable focus, search-help popover, associated hint/status. Test nested-menu arrow keys/Escape and narrow layouts.
- Draft/pills update instantly; **300 ms debounce** for valid requests. Separate draft, committed query, displayed-result query. Reset page on query changes. Stale responses must never look like newer results.
- Incomplete/invalid draft: no request; retain clearly labeled last-valid results, accessible hint, disabled pagination. No prior results: instructional state, not “no matches.” Distinguish loading, refresh error, and empty success; disable pagination for placeholder results.
- URL updates use **replace-on-edit**, matching Organizations; no per-search history checkpoints. Navigation cancels pending debounce and restores text/pills. Invalid drafts do not replace the valid URL. Invalid query opened directly: hint, no request.

## Backend, safety, and data

- Staff-only bounded list/search API; page from users before batch-loading organization display data. No duplicated rows or per-user HTTP/database round trips. Count/page use identical eligibility predicates and deterministic ordering.
- Verify `deleted_at` and `workos_deleted_at` lifecycle semantics; eligible local users have neither deletion marker. Membership eligibility uses `deleted IS FALSE`. Disabled orgs are not deleted memberships.
- All org targets remain accessible despite response bounds: bounded previews/counts and explicit paginated overflow access, never silent truncation.
- Admin MCP: read-only user lookup sharing service/parser; preserve live staff authentication and `admin:read`. Minimal user/org identity results, bounded nested output, no secrets. Provide overflow access where needed.
- **No customer Platform MCP addition:** cross-tenant staff user discovery is intentionally unavailable to customer principals.
- Fictional local/test fixtures: zero/multi-org, missing name/login, deleted user/membership, disabled/duplicate-name orgs. Extend the guarded local admin seed; no public demo expansion solely for this staff page. Verify isolation/idempotency.

## Performance and verification

Current schema has email/lower(email) B-tree indexes and membership lookup indexes, no trigram indexes. No production timings or cardinalities have been measured.

Run LOCAL `EXPLAIN (ANALYZE, BUFFERS)` for page **and count** using documented synthetic sizes: empty/selective/common/no-match terms; one-/two-character terms; mixed/repeated org terms; first/deep pages; high fanout. Add trigram indexes only if measurements justify them and full predicates actually use them. Pagination/debounce are not substitutes for query efficiency.

Acceptance covers grammar parity/round trips, SQL literal matching and same-org semantics, eligibility/dedup/order/bounds, HTTP/MCP authorization, exact dashboard targets, token accessibility, fake-timer debounce/navigation races, empty/error/invalid states, fixture safety, browser styling against Organizations, and measured query plans.

## Planning elaborations

Implementation plan may select concrete finite limits, offset pagination, and a paginated user-organization read to satisfy the approved bounds without hiding targets. These are implementation contracts, not new product workflows.

**Unresolved product questions:** none.
