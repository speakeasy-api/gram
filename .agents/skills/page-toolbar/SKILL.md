---
name: page-toolbar
description: Build the control bar (search, filters, sort, view) on dashboard list/collection pages with the unified `Page.Toolbar` compound component. Activate whenever adding or editing the controls above a list/grid/table on a `client/dashboard` page — a search box, filter chips/sheet, sort dropdown, grid/table view toggle, result count, or a mode toggle — so the page uses `Page.Toolbar` instead of hand-rolled controls. Phrases like "add a search/filter to this page", "filter bar", "sort dropdown", "view toggle", or "let users filter the list" should trigger it.
metadata:
  relevant_files:
    - "client/dashboard/**"
---

# Page.Toolbar — the one control bar for list pages

Every dashboard list/collection page (Catalog, MCP, Costs, Agent Sessions, Employees, Risk Events, the Observe pages) puts its **search + filters + sort + view** controls in a single compound component: **`Page.Toolbar`**. Do NOT hand-roll a search `<input>`, a filter sidebar/popover, a sort `<select>`, a `ViewToggle`, or filter chips — compose the toolbar pieces. This keeps every page visually identical (one contained grey bar, uniform 40px controls, full width, search+filters left / sort+view right) and changeable in one place.

## How to use it

Import `Page` from `@/components/page-layout` and render the toolbar on its **own row below the page title/description**:

```jsx
<Page.Toolbar>
  <Page.Toolbar.Search value={q} onChange={setQ} placeholder="Search…" debounceMs={300} />
  <Page.Toolbar.Filters
    schema={FILTERS} values={values} optionsById={optionsById}
    onChange={setValue} onClear={clearValue} onClearAll={clearAll}
  />
  <Page.Toolbar.SortBy
    value={sort} onChange={setSort}
    options={[{ value: "recent", label: "Recently Added" }]}
    direction={dir} onDirectionChange={setDir}   {/* optional asc/desc toggle */}
  />
  <Page.Toolbar.Count>{count} items</Page.Toolbar.Count>
  <Page.Toolbar.ViewAs value={view} onChange={setView} />
  <Page.Toolbar.Actions>{/* page-specific extras, e.g. a SegmentedControl */}</Page.Toolbar.Actions>
  <Page.Toolbar.Refresh onRefresh={() => void refetch()} isRefreshing={isFetching} />
</Page.Toolbar>
```

The pieces (all optional, written in any order — the toolbar sorts them):

| Piece     | Side  | What it is                                                                                                                                                                                        |
| --------- | ----- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Search`  | left  | Debounced white search box (`debounceMs` optional, built-in clear button)                                                                                                                         |
| `Filters` | left  | Filter chips + "More filters" sheet + "Reset to default"                                                                                                                                          |
| `Leading` | left  | Page-specific left-aligned extras that narrow or re-cut the collection (e.g. a segmented axis track, a scope selector)                                                                            |
| `SortBy`  | right | Sort dropdown, optionally with a built-in asc/desc direction toggle (one bordered box)                                                                                                            |
| `Count`   | right | Result-count text                                                                                                                                                                                 |
| `ViewAs`  | right | Grid/table toggle (grid/table only)                                                                                                                                                               |
| `Actions` | right | Page-specific right-aligned extras                                                                                                                                                                |
| `Refresh` | right | Manual refresh button (`onRefresh` required, `isRefreshing` optional); spins/disables while refreshing and enforces a ~2s minimum visible spin so a fast/cached refetch doesn't look like a no-op |

Layout, height (40px), the grey bar, the search↔filters divider, and the left/right `justify-between` split are all handled by the component — don't re-create them.

A bar with too many controls for one line composes explicit rows inside the same shell instead of hand-rolling a second bar — each `Page.Toolbar.Row` takes the same pieces and lays out the same left/right clusters (see the costs page's `BreakdownBar`):

```jsx
<Page.Toolbar>
  <Page.Toolbar.Row>
    <Page.Toolbar.Search value={q} onChange={setQ} />
    <Page.Toolbar.Actions>{axisTrack}</Page.Toolbar.Actions>
  </Page.Toolbar.Row>
  <Page.Toolbar.Row>
    <Page.Toolbar.Leading>{scopeControls}</Page.Toolbar.Leading>
    <Page.Toolbar.Actions>{exportButton}</Page.Toolbar.Actions>
  </Page.Toolbar.Row>
</Page.Toolbar>
```

## Declaring filters

Filters are a pure `const` schema via `defineFilters` (from `@/components/filters`), driven by `useFilterState(SCHEMA)` (URL-param backed). Kinds: `multiselect` | `select` | `text` | `boolean` | `daterange`.

```ts
import { defineFilters, useFilterState } from "@/components/filters";

const FILTERS = defineFilters([
  {
    id: "date",
    label: "Date range",
    kind: "daterange",
    pinned: true,
    defaultPreset: "30d",
  },
  { id: "status", label: "Status", kind: "multiselect" },
  { id: "policy", label: "Policy", kind: "select" },
]);

const { values, setValue, clearValue, clearAll } = useFilterState(FILTERS);
```

- The schema must be a pure literal (no hooks/fetched data) so `FilterValues<typeof FILTERS>` can derive the typed value object.
- `pinned` dimensions always render a chip (with an "All …" default); others appear only when active and live behind "More filters".
- Pass dynamic option lists (servers, policies, agents) at render via `optionsById` (a `Record<id, {label,value}[]>`) — never bake data into the schema.
- Reuse existing schemas as references: `pages/catalog/catalog-filter-schema.ts`, `pages/mcp/mcp-filter-schema.ts`, and the inline `COST_FILTERS`/`SESSION_FILTERS`/`EMPLOYEE_FILTERS`/`RISK_FILTERS`/`OBSERVE_FILTERS`.

## Bridging to existing query logic

`useFilterState` returns URL-persisted `values` keyed by dimension id. If the page already has its own query/state shape, **bridge** the unified values back to it rather than rewiring every consumer (see catalog's `toCatalogFilterValues`). For pages whose filters already live in the URL (the Observe pages), build a `values` object from the existing reads and route `onChange` to the existing setters.

## Rules

- **Never** hand-roll: a bare `<input>` search, a sort `<select>`, a `ViewToggle` outside the toolbar, or filter chips. Use the pieces.
- **Never** set per-control heights — the toolbar enforces a uniform 40px.
- Two-option **mode toggles** (Tokens/Cost, an Employees/Unknown scope switch, etc.) go in `Page.Toolbar.Actions` using the shared `SegmentedControl` (`@/components/ui/segmented-control`) — give each option a `tooltip`. `ViewAs` is for grid/table only.
- `onClearAll` must reset filters in a **single** `setSearchParams`/`clearAll` call. Firing one setter per filter clobbers in react-router (it reads a memoized snapshot, so the last `navigate` wins) — `useFilterState.clearAll` already does this correctly.
- The "Reset to default" button is built into `Filters`; don't add your own clear button.

## Where things live

- `components/ui/toolbar.tsx` — `Page.Toolbar` + all pieces (the only place to change layout/height/styling).
- `components/ui/segmented-control.tsx` — shared two-or-more option mode toggle.
- `components/filters/` — `filter-schema.ts` (`defineFilters`, types, `chipLabel`, `isDimensionActive`), `useFilterState.ts`, and the chip/sheet/control primitives.

Run `pnpm -F dashboard type-check` after changes.
