---
name: frontend
description: Rules and best practices when working on the dashboard React frontend codebase (including the inlined Gram Elements code)
metadata:
  relevant_files:
    - "client/dashboard/**"
---

## React & Frontend Coding Guidelines

### Verification Commands

Use `pnpm` package scripts for frontend checks. From the repo root, prefer `pnpm -F <package> <script>` so commands run against the right frontend package without `cd`. Do not run `npm exec`, `npx`, bare `vitest`, bare `eslint`, bare `oxfmt`, or bare `tsc` unless you are debugging the package script itself.

| Need                | Dashboard                       | Whole workspace                             |
| ------------------- | ------------------------------- | ------------------------------------------- |
| Package lint gate   | `pnpm -F dashboard lint`        | `pnpm lint`                                 |
| Type-check only     | `pnpm -F dashboard type-check`  | `pnpm type-check`                           |
| Tests once          | `pnpm -F dashboard test`        | Run the touched package's `pnpm test`       |
| Tests in watch mode | `pnpm -F dashboard test:watch`  | Run the touched package's `pnpm test:watch` |
| ESLint only         | `pnpm -F dashboard lint:eslint` | Run the touched package's script            |
| Format check only   | `pnpm -F dashboard lint:format` | Run the touched package's script            |

For small edits, run the narrowest package script that proves the change. For shared or cross-package frontend changes, run the root `pnpm lint` and `pnpm type-check` scripts.

### General Guidelines

- Use the `pnpm` package manager
- When interacting with the server, use the `@gram/client` package (this is an alias to `client/dashboard/src/sdk/src/...`)
- The document `client/dashboard/src/sdk/REACT_QUERY.md` is very helpful for understanding how to use React Query hooks that come with the SDK.
- For data fetching and server state, use `@tanstack/react-query` instead of manual `useEffect`/`useState` patterns
- When invalidating React Query caches after mutations, invalidate ALL relevant query keys — not just the most specific one. Different hooks may use different query key prefixes for the same data (e.g., `queryKeyInstance` vs `toolsets.getBySlug`). Use broad invalidation helpers like `invalidateAllToolset(queryClient)` to ensure all consumers refresh.

### Telemetry & observability data fetching

Observability surfaces — Tool Logs, insights/analytics, summary cards, counts, and filter dropdowns — are served from **pre-aggregated ClickHouse summary views** behind endpoints like `telemetry.getToolUsageSummary`, `telemetry.listToolUsageTraces`, and `telemetry.query`. **Default to these summary-backed endpoints; they're fast and are the right choice for almost every view.**

Per-log detail is the rare exception. Free-text search over log bodies, arbitrary custom-attribute filters (e.g. `@user.region`), and inspecting an individual log/trace fall back to scanning the raw `telemetry_logs` table, which is slow. Only reach for those when detailed telemetry is genuinely what the user needs — not for default lists, summaries, or filter options.

When you add a control that triggers the raw-log path (a free-text search box, a custom-attribute filter), tell the user it may be slower — see `SlowSearchNotice` in `LogsTools.tsx`, shown only while such a filter is active — and keep the structured filters (server, user, agent, type, date) on the fast summary path. Don't build a default-on view whose first paint requires a raw-log scan.

### Component Structure and Reuse

**The core rule: every UI pattern that appears in more than two places must be centralized so it can be changed in a single location.**

#### Check `components/` before writing anything

Before writing any JSX for a UI element, check `client/dashboard/src/components/` for an existing component. This includes layout wrappers, table headers, empty states, filter pill groups, search inputs, badges, cards — anything. Reuse what exists. Never create a one-off `<div className="...">` when a named component already exists for that purpose.

If no component exists and you expect the pattern to appear in more than a few places across the app, **create one** in `client/dashboard/src/components/` before using it. Name it for what it _is_, not where it happens to appear first (e.g., `PageTabsTrigger`, not `SourceDetailTabTrigger`).

#### No duplicated className strings

If the same Tailwind className string (or any meaningful substring of one) appears on 3+ elements anywhere in the codebase, extract it to:

- A component's built-in styling
- A `cva` variant
- A named `const` used in `cn()`

The symptom to watch for: copy-pasting a `className` prop. That is always wrong.

#### No duplicated JSX blocks

If you find yourself copy-pasting a JSX structure — even with minor variations — stop and extract a parameterized component. Three near-identical blocks is the threshold.

#### No IIFEs in JSX

Never use immediately-invoked function expressions inside JSX (`{(() => { ... })()} `). Extract to a named sub-component or a variable above the return statement.

#### No multi-line or nested ternaries

A ternary that wraps onto multiple lines, or chains a second `?:` inside a branch, is unreadable. Reach for one of these instead:

- A `switch` statement when mapping a discriminator (e.g. a `kind` / `type` string) to one of several values.
- A small **named helper function**, hoisted to module scope when the mapping is pure (e.g. converting a frontend enum to a backend constant). The helper labels the intent at the call site and the JSX stays flat.
- An object lookup when the keys are statically known and the values are simple constants.

```tsx
// ❌ wrong — nested multi-line ternary
attachmentType={
  sourceKind === "function"
    ? "functions"
    : sourceKind === "externalmcp" || sourceKind === "remotemcp"
      ? "external_mcp"
      : "openapi"
}

// ✅ right — hoisted helper with a switch
function attachmentTypeForSourceKind(sourceKind: string | undefined): string {
  switch (sourceKind) {
    case "function":
      return "functions";
    case "externalmcp":
    case "remotemcp":
      return "external_mcp";
    default:
      return "openapi";
  }
}

attachmentType={attachmentTypeForSourceKind(sourceKind)}
```

Single-line, single-branch ternaries (`isOpen ? "x" : "y"`) are fine.

#### Keep components focused

A component that has grown past ~150 lines of JSX is doing too much. Break it up. If a page has multiple tabs, each tab's content is its own component.

#### Page headers and subtext are a common duplication trap

Many pages render the same `<h1>` + `<p>` header block in 2–3 conditional render paths (loading skeleton, empty state, populated state). Examples observed: `InsightsTools.tsx`, `LogsTools.tsx`, `LogsAgents.tsx`, `SecurityOverview.tsx`, `PolicyCenter.tsx`. Symptoms: a copy change touches the same string in 3 places; `Edit` with `replace_all` fails because indentation differs between the duplicates.

When adding or editing page headers, lift the title and subtitle into a small `<PageHeader title="…" subtitle="…" />` (or pass them as props to a shared shell), not into each render branch. When editing existing duplicated copy, target a unique trailing fragment of the string (e.g. `"in chat messages."`) so a single `replace_all` covers every copy regardless of indentation — and file a follow-up to extract a shared header.

#### Shared empty states: props with defaults, not forks

When the same empty-state component is reused across pages but needs different copy per caller (e.g. `HooksEmptyState` rendered from both `/insights/tools` and `/logs/tools`), add optional `title` / `subtitle` props with sensible defaults rather than forking the component:

```tsx
export function HooksEmptyState({
  title = "No logs captured",
  subtitle = "Install Observability plugin in your AI agent to start capturing tool execution logs",
}: { title?: string; subtitle?: string } = {}) {
  /* … */
}
```

Backwards-compatible callers stay `<HooksEmptyState />`; only the variant caller passes overrides. Avoids divergent copies of the surrounding scaffolding (provider cards, setup dialogs, etc.).

### Tables

Use the design system `Table` from `@/components/ui/Table` for dashboard tables. Do **not** add new shadcn table wrappers or hand-roll table styling with raw `<table>` markup when `Table` can express the UI. If you find a lingering legacy table pattern, migrate it when touched.

```tsx
import { Column, Table } from "@/components/ui/Table";
```

For normal data tables, prefer the declarative `columns` / `data` / `rowKey` API. Define `Column<T>[]` near the component so render functions stay typed, use `render` for rich cells, and use `width` for stable layouts instead of ad hoc cell class widths.

```tsx
const columns: Column<Role>[] = [
  {
    key: "name",
    header: "Name",
    width: "180px",
    render: (role) => <Type className="font-medium">{role.name}</Type>,
  },
  {
    key: "members",
    header: "Members",
    width: "100px",
    render: (role) => <Type>{role.memberCount}</Type>,
  },
];

<Table columns={columns} data={roles} rowKey={(row) => row.id} />;
```

For empty and loading states, use the Table's built-in empty surface and the shared `SkeletonTable` from `@/components/ui/skeleton`. Do not rebuild a one-off empty `<tbody>` or skeleton table.

```tsx
<Table
  columns={columns}
  data={filteredKeys}
  rowKey={(row) => row.id}
  className="max-h-[500px] overflow-y-auto"
  noResultsMessage={<Type>No matching API keys</Type>}
/>
```

Search and filter controls are siblings above the table. On pages, wrap them in `Page.Toolbar` (see the `page-toolbar` skill); the bare `Stack` form below is for non-page surfaces like dialogs and sheets. Keep filter state outside the table, derive filtered rows with `useMemo`, and pass the result to `data`. Use existing controls such as `SearchBar`, `MultiSelect`, `Select`, or page-specific filter pills; do not put form controls inside `Table.Header` unless they are truly column headers. If the table is paginated, reset the page index when filters change.

```tsx
const [search, setSearch] = useState("");
const [selectedTags, setSelectedTags] = useState<string[]>([]);

const filteredRows = useMemo(() => {
  const normalizedSearch = search.trim().toLowerCase();

  return rows.filter((row) => {
    const matchesSearch =
      normalizedSearch.length === 0 ||
      row.name.toLowerCase().includes(normalizedSearch);
    const matchesTags =
      selectedTags.length === 0 ||
      row.tags.some((tag) => selectedTags.includes(tag));

    return matchesSearch && matchesTags;
  });
}, [rows, search, selectedTags]);

<Stack direction="horizontal" gap={2} className="mb-4 h-fit">
  <SearchBar
    value={search}
    onChange={(value) => {
      setSearch(value);
      setPage(0);
    }}
    placeholder="Search tools"
    className="w-64"
  />
  <MultiSelect
    options={tagOptions}
    defaultValue={selectedTags}
    onValueChange={(value) => {
      setSelectedTags(value);
      setPage(0);
    }}
    placeholder="Filter by tag"
    autoSize
  />
</Stack>

<Table
  columns={columns}
  data={filteredRows}
  rowKey={(row) => row.id}
  noResultsMessage={<Type>No matching tools</Type>}
/>;
```

Footers that summarize, paginate, or load more rows should usually be sibling bars immediately below the table. The table API does not require a special footer component for this; keep the table declarative and put pagination/load-more controls after it.

```tsx
<Table columns={columns} data={visibleRows} rowKey={(row) => row.id} />;

{
  totalPages > 1 && (
    <div className="flex items-center justify-between border-t px-4 py-3">
      <Type className="text-muted-foreground text-sm">
        {pageStart}-{pageEnd} of {filteredRows.length}
      </Type>
      <div className="flex items-center gap-1">
        <Button
          variant="tertiary"
          size="sm"
          onClick={() => setPage((page) => page - 1)}
          disabled={page === 0}
        >
          Previous
        </Button>
        <Button
          variant="tertiary"
          size="sm"
          onClick={() => setPage((page) => page + 1)}
          disabled={page >= totalPages - 1}
        >
          Next
        </Button>
      </div>
    </div>
  );
}
```

Use the compound API only when the body needs custom structure that the declarative API cannot express, such as mixed rows, a full-width CTA row, or a custom no-results branch. Keep the design system wrapper, header, row, and cell components as the default primitives.

```tsx
<Table columns={columns}>
  <Table.Header columns={columns} />
  {items.length === 0 ? (
    <Table.NoResultsMessage>No results found.</Table.NoResultsMessage>
  ) : (
    <Table.Body>
      {items.map((item) => (
        <Table.Row key={item.id} row={item} columns={columns} />
      ))}
    </Table.Body>
  )}
  <Table.Row>
    <div className="border-border bg-muted/20 col-span-full border-t py-5 text-center">
      <Type className="text-muted-foreground text-sm">
        Want to grant new members access?
      </Type>
      <Button variant="tertiary" size="sm" className="mt-2">
        Configure Roles
      </Button>
    </div>
  </Table.Row>
</Table>
```

Use grouped or expandable rows through the table props instead of nesting unrelated cards or custom accordions around a table. Current patterns use `hideHeader` for grouped parent rows and `renderExpandedContent` for nested details.

```tsx
<Table
  columns={groupColumns}
  data={groups}
  rowKey={(row) => row.key}
  hideHeader
  renderExpandedContent={(group) => (
    <Table
      columns={childColumns}
      data={group.items}
      rowKey={(row) => row.id}
      hideHeader
    />
  )}
/>
```

Raw `<tr>` / `<td>` should be rare and stay inside a `<Table.Body>` only when native table semantics are needed and `Table` does not expose them, such as a `colSpan` overflow row. If the row is a normal data row, use `<Table.Row row={row} columns={columns} />` or the declarative `data` prop.

### React Performance Patterns

These patterns were established in the audit log (#2140) and deployment log (#2167) redesigns. Apply them whenever building search, filtering, or keyboard navigation.

#### Hoist RegExp creation

Never create `new RegExp()` inside a render callback (e.g., `highlightMatch`). Extract it to a `useMemo` keyed on the search query:

```typescript
const searchRegex = useMemo(() => {
  if (!searchQuery) return null;
  const escaped = searchQuery.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return new RegExp(`(${escaped})`, "gi");
}, [searchQuery]);
```

Then use `searchRegex` inside `useCallback`-wrapped functions.

#### Defer expensive search computations

Wrap search queries with `useDeferredValue` before passing them to expensive `useMemo` computations (e.g., filtering all logs). This keeps the input responsive while React defers the downstream recomputation:

```typescript
const deferredSearchQuery = useDeferredValue(searchQuery);
const filteredIndices = useMemo(() => { /* expensive filter */ }, [deferredSearchQuery, ...]);
```

#### Derive state during render, not via useEffect

If a value can be computed from current state, derive it inline — don't sync it via `useEffect`. This prevents a flash of stale values between renders:

```typescript
// DO: derive during render
const effectiveSearchIndex =
  searchMatchIndices.length > 0
    ? Math.min(currentSearchIndex, searchMatchIndices.length - 1)
    : 0;

// DON'T: clamp via useEffect (causes stale render flash)
useEffect(() => {
  if (currentSearchIndex >= searchMatchIndices.length) setCurrentSearchIndex(0);
}, [searchMatchIndices.length]);
```

#### Reset navigation state on data changes

When a component has keyboard navigation (j/k/g/G) with a `currentIndex` state, reset it when the underlying data changes (filters, pagination, data refresh):

```typescript
useEffect(() => {
  setCurrentLogIndex(null);
}, [logs]); // or parsedLogs, depending on the component
```

### Tooltip Usage

`App.tsx` wraps the entire app in a global `TooltipProvider`. **Never add another `TooltipProvider` inside a component** — doing so creates a redundant Radix context per instance and contributes to `ResizeObserver loop completed with undelivered notifications` errors in the browser.

Use `<Tooltip>`, `<TooltipTrigger>`, and `<TooltipContent>` directly — they inherit the global provider automatically. For simple cases use the existing `<SimpleTooltip tooltip="...">` wrapper from `@/components/ui/tooltip`.

```tsx
// ✅ correct
<Tooltip>
  <TooltipTrigger asChild>{button}</TooltipTrigger>
  <TooltipContent>Hello</TooltipContent>
</Tooltip>

// ❌ wrong — TooltipProvider already exists at the app root
<TooltipProvider>
  <Tooltip>
    <TooltipTrigger asChild>{button}</TooltipTrigger>
    <TooltipContent>Hello</TooltipContent>
  </Tooltip>
</TooltipProvider>
```

### Navigation and Links

Use the right primitive for the link type — mixing them causes full-page reloads, broken multi-tenancy, or missing security headers.

**Internal navigation (any URL inside the dashboard):** Use the route helpers from `client/dashboard/src/routes.tsx`. Top-level routes _and_ subpages expose `.Link`, `.href()`, and `.goTo()`:

```tsx
// Wrap a child node with .Link
<routes.sources.Link>
  <Button>Connect a Source</Button>
</routes.sources.Link>

// Subpages get .Link too — use it instead of building strings
<routes.insights.tools.Link>
  <Button>Track AI usage</Button>
</routes.insights.tools.Link>

// Plain react-router Link with .href() when you need a className or are inside <p>
<Link to={routes.plugins.href()} className="underline underline-offset-2">
  Observability plugin
</Link>
```

Never hardcode org/project slugs in an `href` (e.g. `https://app.getgram.ai/speakeasy-team/projects/default/plugins`). The route helpers resolve the current `:orgSlug` / `:projectSlug` from the URL, so the same call works for every tenant.

**External links (anywhere outside the dashboard):** Use a plain `<a>` with `target="_blank"` and `rel="noopener noreferrer"`. This matches the existing pattern (`AddServerDialog.tsx:1162`, `CatalogDetail.tsx:229`) and the security attributes are mandatory — `noopener` blocks `window.opener` access; `noreferrer` strips the Referer header.

```tsx
<a
  href="https://www.speakeasy.com/product/mcp-gateway/catalog"
  target="_blank"
  rel="noopener noreferrer"
  className="underline underline-offset-2 hover:text-foreground"
>
  MCP Registry
</a>
```

The `@/components/ui/link` wrapper sets `target="_blank"` when `external` is true but also injects an icon — fine for nav rows, too heavy for inline links inside subtext. Reach for the plain `<a>` for inline external links.

### Editing copy

- **Always use American (US) English spelling in user-visible copy.** Every string the user reads — labels, headings, buttons, tooltips, placeholders, empty states, toasts, error messages — uses American spelling, per the Speakeasy brand style guide. Highest-frequency cases: `color` not `colour`, `canceled`/`canceling` not `cancelled`/`cancelling`, `behavior` not `behaviour`, `license` (noun and verb) not `licence`, `catalog` not `catalogue`, `gray` not `grey`, `center` not `centre`, `analyze` not `analyse`, and `-ize`/`-ization` endings (`organize`, `customize`, `authorization`, `synchronization`) not `-ise`/`-isation`. This applies only to what the user reads — leave code identifiers, API field names, and third-party tokens alone even when they use British spelling (e.g. an upstream `colour` field stays `colour`; only the visible label is Americanized).
- **Preserve dynamic tokens.** Page subtext often interpolates state like `{rangeLabel}`, `{periodUsage.credits}`, or `{projectName}`. When rewording copy that contains a token, keep the token in place — replace it with the literal current value (e.g. "the last 30 days") only when the data fetch itself is locked to that value. Otherwise the copy starts lying as soon as the user changes a filter.
- **Don't fight the Tailwind class sorter.** Prettier's `prettier-plugin-tailwindcss` reorders classes on save. Write classes in any order; the formatter will normalize them and the diff stays clean across the codebase.
- **AI context strings shadow user-visible names.** When renaming a chart or card (e.g. "Most Used LLM Clients" → "Most Used Agents"), search for the old name in nearby `contextInfo=` / `suggestions=` props passed to `ExploreWithAI` / `InsightsConfig`. Those strings are sent to the LLM as analytical context; if they drift from the visible label, the AI assistant talks about a card the user can't see.

### Building a new page (required pattern)

**First choice: a page template.** Do not hand-roll the `Page` frame. Pick the template that matches the page's shape from `@/components/page-templates` and fill in data — it owns the frame, breadcrumbs, scope gate, the single header, and the loading/empty/error branches (which is what stops the "three-branch header" duplication). Full recipes: `client/dashboard/src/components/page-templates/README.md`.

| Page shape                                                                 | Template                       |
| -------------------------------------------------------------------------- | ------------------------------ |
| collection: search/filter + table or card grid + empty state               | `ResourceListPage`             |
| one entity: hero + sections (rail wired via app sidebar)                   | `DetailPage`                   |
| tabs that are _different resources_ (not one entity's sections)            | `TabbedPage`                   |
| a single create/edit `<form>`                                              | `FormPage`                     |
| stacked titled config sections (or a prose column via `variant="content"`) | `SettingsPage`                 |
| dashboard: stat row + summary/chart cards                                  | `OverviewPage`                 |
| fullbleed analytics/observe surface (sticky filters + big table/charts)    | `WorkbenchPage`                |
| multi-step flow                                                            | `WizardPage`                   |
| auth / standalone, outside the app shell                                   | `CenteredPage`                 |
| genuine bespoke app canvas (chat, playground, builder)                     | `FullBleedPage` (escape hatch) |

```tsx
import { ResourceListPage } from "@/components/page-templates";

// Gate the DATA-OWNING component from outside so its query never fires for
// unauthorized users. A data hook called in the same component that renders the
// template runs BEFORE the template's own `scope` gate — so wrap it here.
export default function Environments(): JSX.Element {
  return (
    <RequireScope scope="project:read" level="page">
      <EnvironmentsInner />
    </RequireScope>
  );
}

function EnvironmentsInner(): JSX.Element {
  const q = useEnvironments(); // runs only after the page gate above passes
  const rows = q.data ?? [];
  // Write affordances get their OWN component-level gate — the page scope is
  // read, so an any-of page scope must never be what hides a write button.
  const newButton = (
    <RequireScope scope="project:write" level="component">
      <Button>New environment</Button>
    </RequireScope>
  );
  return (
    <ResourceListPage
      title="Environments"
      description="One-line purpose."
      primaryAction={rows.length > 0 ? newButton : undefined}
      isLoading={q.isPending}
      isEmpty={rows.length === 0}
      empty={{
        icon: "blocks",
        heading: "No environments yet",
        action: newButton,
      }}
    >
      <Table columns={columns} data={rows} rowKey={(r) => r.id} />
    </ResourceListPage>
  );
}
```

**Scope gating (important):** wrap the data-owning component in `RequireScope` (the view scope, e.g. `project:read`) and call data hooks inside it, so the query never fires for unauthorized users. The templates also accept a `scope` prop, but it only gates _rendering_ — a hook in the same component that renders the template runs before that gate, so prefer the outer wrapper for anything that fetches. Gate write-only affordances (create/delete buttons, mutation tabs) with their own `level="component"` `RequireScope` for the write scope; never rely on an any-of page scope to hide them.

Only a page that fits **none** of the templates falls back to the raw skeleton (and it still must render the header exactly once — no bespoke `<h1>`):

```tsx
import { Page } from "@/components/page-layout";

export default function MyNewPage(): JSX.Element {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Page.Section>
          {/* Area eyebrow (OBSERVE/SECURE/CONNECT/DISTRIBUTE/ORGANIZATION, from
              the URL) over a thin serif title. area="…" overrides, area="" suppresses. */}
          <Page.Section.Title>My New Page</Page.Section.Title>
          <Page.Section.Description>One-line purpose.</Page.Section.Description>
          <Page.Section.Body>{/* content */}</Page.Section.Body>
        </Page.Section>
      </Page.Body>
    </Page>
  );
}
```

Checklist for the content below the title:

- **One page title per page.** Secondary sections get `text-eyebrow` overlines (utility groupings, table/list sections) or a smaller serif `text-display-xs` (content subsections with their own body) — never a second eyebrow + full-size serif stack.
- Stat rows → `StatRow` from `@/components/stat-row` (a `MetricCard.Group` with a built-in `isLoading` → skeleton swap); pass `metrics` with explicit `tone`s. Drop to raw `MetricCard` in `MetricCard.Group` only for a bespoke row.

  ```tsx
  <StatRow
    isLoading={q.isPending}
    metrics={[
      { label: "Total rules", value: total, tone: "information" },
      {
        label: "Violations",
        value: n,
        tone: n > 0 ? "destructive" : "neutral",
        delta: "+3",
        description: "last 7 days",
      },
    ]}
  />
  ```

- Tables → design-system `Table` (headers come out as eyebrows for free); hand-rolled grids use `text-eyebrow` header labels.
- List/filter controls → `Page.Toolbar` (see the `page-toolbar` skill), mono uppercase segments for mode switches.
- Empty states → **`InlineEmptyState`** from `@/components/inline-empty-state` (`icon`/`graphic` + `heading` + `description` + `action`, `orientation="horizontal"` variant) for empty regions inside a page; `EmptyState` from `@/components/page-layout` for full-page voids. **Never hand-roll** the `border-dashed` + `rounded-full` icon-blob block — `InlineEmptyState` is the square-hairline-tile idiom and the templates' `empty` prop routes through it.
- Chart/summary panels → `ChartCard` from `@/components/chart/ChartCard` (titled panel with loading/error states); the detail-column width wrapper is `DetailBody` from `@/components/detail-body` (never re-type `max-w-[1270px] px-8 py-8`).
- Loading → content-shaped skeletons (`SkeletonTable`, geometry-matched rows), never a lone spinner or a premature empty state.
- Cards white (`bg-card`), page gutter gray, hairline borders, no shadows/gradients/washes, square corners — per the styling rules below.

A page that renders its own `<h1>` instead of this pattern is a defect; if a custom header is unavoidable, it must still render `<PageEyebrow />` + `text-display-sm font-thin`.

### Design-system inventory (consolidated — don't import the removed ones)

The primitive shelf was consolidated; these imports are gone or moved. Reaching for a removed one is a defect:

- **`MetricCard`** — one primitive only: `@/components/ui/MetricCard` (`label`/`value`/`tone`). The analytics tile that formats numbers/thresholds/deltas is **`StatTile`** (`@/components/chart/stat-tile`, `StatTile`/`StatTileGroup`) — _not_ a second `MetricCard`.
- **Password inputs** — no `PrivateInput`; use `<Input type="password" reveal />` (the `reveal` prop adds the show/hide eye toggle).
- **`DashboardCard`** — now `Card.Dashboard` (`title`/`action`/`tooltip` + children).
- **`ToggleButton`** — imported from `@/components/ui/SegmentedControl` (re-homed); use `SegmentedControl` for a full option group.
- **`Modal` / `IconButton`** — removed. Use `Dialog` for modals; `Button` with the `icon` prop for icon-only buttons.
- **Detail-page shared bones** live in `@/components/detail/`: `SettingsSection`/`DangerSettingsSection` (`settings-section`, also re-exported from the `page-templates` barrel) and `DetailSidebarNav`/`DetailSidebarInfoLabel` (`detail-sidebar-nav`) — not under `pages/mcp/...` and not `Mcp`-prefixed.

### Styling and Design System

The dashboard follows an editorial, print-like design language. The load-bearing rules:

- **Square corners.** The Tailwind radius scale is wiped (`--radius-*: initial` in `App.css`), so `rounded-sm/md/lg/...` generate nothing — never write them. `rounded-full` is reserved for true circles (avatars, status dots, spinners); pills on wide elements are off-style.
- **Flat.** No `shadow-*` on in-flow surfaces (cards, buttons, inputs, tiles, sticky bars). Shadows are allowed only on floating overlays (menus, dialogs, tooltips, sheets). No gradients, no colored tint washes (`bg-blue-500/10`, `bg-amber-100`, `bg-*-softest` panels) — express semantics with colored text, borders, or a small dot on a neutral surface.
- **Hairline borders** via the default border token; the content area is a white `bg-card` sheet on the gray page gutter. Beware: `bg-background` is the page-gray token, NOT white — use `bg-card` for white surfaces.
- **Page pattern**: every page shows an area micro-label + thin serif title. `Page.Section.Title` renders both automatically (`area` prop overrides, `""` suppresses); custom headers render `<PageEyebrow />` from `@/components/page-eyebrow` above an `h1` with `text-display-sm font-thin`. The area derives from the URL via `useNavArea()` — one source shared with the sidebar highlight.
- **`text-eyebrow`** (mono 11px uppercase tracked muted utility) is THE style for table headers, section overlines, and stat labels. Never hand-roll `text-xs font-medium uppercase tracking-*`.
- **Stat tiles**: `MetricCard` from `@/components/ui/MetricCard` inside `MetricCard.Group` (one bordered strip, hairline dividers; the Group lays tiles side by side — render one `MetricCard` per stat inside it). `tone` is a required prop (TypeScript errors if omitted — there is no default) — declare a semantic tone (counts → `information`, health → `success`, errors → conditional `destructive`, blocked/stale → conditional `warning`, `neutral` only when nothing applies).
- **Charts** import colors from `@/components/chart/palette` (exports `SERIES` — ink + muted brand hues, `ACCENT_RED` for risk/error only, `TOOLTIP` — square Chart.js tooltip styling, and `AXIS` tokens). Components resolve the themed ramp with `useSeriesColors()` from `@/components/chart/useSeriesColors`; pure data builders take a `colors` param instead of calling hooks.
- **Avatars/initials** use `getIdentityTint(label)` from `@/components/gradient-colors` — never random-hue or saturated gradient fills.
- **Tabs**: segmented `ui/Tabs`/`SegmentedControl` (mono uppercase, solid-ink active) for mode switches; `PageTabsList` + `PageTabsTrigger` (flush underline, no outer box) for page-level tabs. Pairing plain `TabsList` with `PageTabsTrigger` draws a boxed underline hybrid — wrong.
- **ALWAYS use the design system** in `@/components/ui`; every component lives in its own directory with an `index.stories.tsx` beside it; add a story whenever you add a component.
- **NEVER use hardcoded Tailwind colors** like `bg-neutral-100`, `border-gray-200`, `text-gray-500`, `bg-emerald-*`, etc. — tokens only.
- `@tailwindcss/typography` must remain in `devDependencies` — the dashboard uses `prose` and `not-prose` classes directly (e.g. `CatalogDetail.tsx`, `tool.tsx`) which are provided by this plugin.
- Tailwind v4's Vite plugin registers classes from NEW files only at server start — restart the dev server / Storybook after adding a file with arbitrary classes, or they silently emit nothing.

### Release Stage Badges (Preview / Beta)

Pre-GA features get a `Preview` or `Beta` badge wherever the user would otherwise mistake the feature for being GA. The same `ReleaseStageBadge` component renders on every surface, so labels never drift.

**Source of truth:** `client/dashboard/src/components/release-stage-badge.tsx` — exports `ReleaseStageBadge` and the `ReleaseStage = "preview" | "beta"` type.

**Underlying primitive:** the design system `<Badge>` (`@/components/ui/Badge`). `ReleaseStageBadge` composes it with `background` enabled — this is the source of truth for shape (mono, uppercase, tracked, hairline-bordered, square). Do **not** override these classes; the design system owns them. The wrapper just picks a semantic variant and adds a tooltip.

**Variant → stage mapping** (variant names are hooks, not literal semantics):

- `preview` → the `warning` variant (amber).
- `beta` → the `information` variant (Speakeasy brand blue).

> The badge variants (`neutral | destructive | information | success | warning`) are tuned for alert/feedback contexts, but the names are just hooks — `warning` here means "experimental, use with caution," not "alert." That's the intended way to reuse the palettes; don't invent new variants without design buy-in.

**Never hardcode Tailwind colors** (no `bg-violet-500`, no raw `bg-warning-softest` spans). If you find yourself reaching for raw classes for a new badge use case, that's a signal to either pick an existing variant or add one to `@/components/ui/Badge`.

#### Surface 1 — sidebar nav (route-driven)

Set `stage` on the route declaration. `app-sidebar.tsx` forwards `item.stage` through `ScopeGatedNavItem → NavButton`, which renders the badge with a hover tooltip explaining what the stage means. The badge auto-hides in collapsed-icon mode.

```tsx
// client/dashboard/src/routes.tsx
assistants: {
  title: "Assistants",
  url: "assistants",
  icon: "bot",
  stage: "preview", // ← sidebar pill appears automatically
  component: AssistantsRoot,
},
```

> **Gotcha**: if you ever introduce another sidebar wrapper that calls `NavButton` directly (instead of going through `NavMenuButton`), you must forward `stage={item.stage}` explicitly. `app-sidebar.tsx`'s `ScopeGatedNavItem` does this — copy that pattern.

#### Surface 2 — page section title

Pass `stage` on the **primary** `Page.Section.Title` for the page (usually the first `Page.Section` under `Page.Body`). Don't put it on secondary section titles like "Recent Chats" — the badge labels the whole feature, not individual sections.

```tsx
<Page.Section>
  <Page.Section.Title stage="beta">Risk Overview</Page.Section.Title>
  <Page.Section.Description>…</Page.Section.Description>
</Page.Section>
```

> **Gotcha**: pages with multiple render branches (loading / empty / populated) must include the title — and therefore the `stage` — in **every** branch. `PolicyCenter.tsx` and `SecurityOverview.tsx` are the reference for this pattern.

#### Surface 3 — tab nav (sub-route tabs)

For `ObserveTabNav`-style tab strips, add `stage` to the local tab descriptor and render `<ReleaseStageBadge size="xs" noTooltip>` inline. Use `inline-flex items-center gap-2` on the tab `<Link>` so the badge tracks the label without disrupting the active-tab underline.

```tsx
// client/dashboard/src/components/observe/ObserveTabNav.tsx
type Tab = { label: string; href: string; stage?: ReleaseStage };
const tabs: Tab[] = [
  { label: "Employees", href: `${baseSlug}/employees`, stage: "preview" },
];
```

#### Surface 4 — raw `<h1>` headings (pages that don't use Page.Section.Title)

A handful of pages render their own `<h1 className="text-xl font-semibold">…</h1>` instead of `Page.Section.Title` (e.g., `InsightsEmployees`, `InsightsAgents`). Wrap the heading and the badge in a `flex items-center gap-2` div:

```tsx
<div className="flex items-center gap-2">
  <h1 className="text-xl font-semibold">AI Agent Costs</h1>
  <ReleaseStageBadge stage="preview" />
</div>
```

> Consider migrating these pages to `Page.Section.Title` in a follow-up — but don't bundle that refactor with the badge addition.

#### Which surfaces does a given feature need?

- **Default**: every surface where the user encounters the feature's name. If it has a sidebar entry **and** a page heading, badge both.
- **Tab-only feature**: badge the tab. The parent route's nav entry stays clean since the parent isn't itself pre-GA.
- **Hidden behind a feature flag**: still badge the visible surfaces. The flag controls visibility; the badge communicates stage to users who can see it.

#### Removing a badge (feature ships GA)

Grep `stage="preview"` and `stage="beta"` and delete every match:

- `routes.tsx` — remove the `stage:` field on the route entry
- `Page.Section.Title stage="…"` — drop the prop
- `ObserveTabNav` (or similar) tab descriptors — drop the `stage` field
- Inline `<ReleaseStageBadge>` usages — delete the element and unwrap the `flex items-center gap-2` div

There's no other cleanup. The component itself stays in place for the next pre-GA feature.

### Drawer Component Usage

`DrawerContent` requires `DrawerTitle` (and optionally `DrawerDescription`) inside it. Omitting them generates a console error and breaks screen reader accessibility. `DrawerHeader` and `DrawerFooter` are optional layout wrappers.

```tsx
<Drawer>
  <DrawerTrigger>Open</DrawerTrigger>
  <DrawerContent>
    <DrawerTitle>Session Details</DrawerTitle>
    <DrawerDescription>Viewing trace for this chat.</DrawerDescription>
    {/* content */}
  </DrawerContent>
</Drawer>
```

If the title is visually redundant, hide it from sighted users while keeping it for screen readers:

```tsx
<DrawerTitle className="sr-only">Details</DrawerTitle>
```

### Dialog Component Usage

`DialogContent` requires `DialogTitle` (and optionally `DialogDescription`) inside it. Omitting them generates a console error and breaks screen reader accessibility. `DialogHeader` and `DialogFooter` are optional layout wrappers.

```tsx
<Dialog>
  <DialogTrigger>Open</DialogTrigger>
  <DialogContent>
    <DialogTitle>Confirm Action</DialogTitle>
    <DialogDescription>This cannot be undone.</DialogDescription>
    {/* content */}
  </DialogContent>
</Dialog>
```

If the title is visually redundant, hide it from sighted users while keeping it for screen readers:

```tsx
<DialogTitle className="sr-only">Details</DialogTitle>
```
