# Merge "MCP & Tools" and "Tool Logs" into one tabbed Observe page

**Date:** 2026-07-31
**Status:** Design approved, pending spec review

## Problem

The dashboard exposes two separate pages over the _same_ telemetry data:

- **MCP & Tools** (`/insights`) — the aggregate "insight" view: totals, time-series, and target/user breakdowns.
- **Tool Logs** (`/logs`) — the row-level view: individual tool-invocation traces.

A user who sets a filter on one page almost always wants the same filter on the other. Today that means re-entering the filter after navigating, because the two routes are independent pages that don't carry state across the jump. There is also no path from "I see a spike in the chart" to "show me the individual calls behind it" — the user has to eyeball the filters and re-type them.

This split is artificial. Both pages are already built on the same machinery.

## What's already shared (why this is low-friction)

Confirmed by reading the current code:

- **Same filter bar** — both render `ObserveFilterBar` (`components/observe/ObserveFilterBar.tsx`).
- **Same URL-synced filter hook** — both use `useObserveFilters` (`components/observe/useObserveFilters.ts`), which serializes to URL search params with identical key names: `range`/`from`/`to`/`label`, `server`, `user`, `source`, `role`, `hookTypes`, `account_type`.
- **Same backend + payload shape** — both send `{ from, to, hostedToolsetSlugs, shadowServerNames, targetTypes, userFilters, hookSources, accountType }`, and both fetch dropdown options from the same endpoint `telemetryGetToolUsageFilterOptions`.

The only real differences:

- **Which endpoint consumes the payload.** MCP & Tools calls the aggregate `telemetryGetToolUsage*` family (`…Totals`, `…Targets`, `…Users`, `…TargetTimeSeries`, `…UserTimeSeries`, `…UsersByTarget`, `…TargetToolBreakdown`). Tool Logs calls the row-level `telemetryListToolUsageTraces` (paginated `useInfiniteQuery`).
- **Two extra filter axes on Tool Logs only:** `status` (via `useObserveFilters`) and attribute-search (`q` free-text + serialized `af` attribute filters, via `useAttributeSearchParams` and `LogFilterBar`).

So the two pages share the same data domain and the same core filter vocabulary; they diverge only in aggregate-vs-row endpoint plus the logs-only status/attribute axes.

## Current files (as of this design)

- `client/dashboard/src/pages/insights/Insights.tsx` — `InsightsRoot` shell + `InsightsHooksPage`.
- `client/dashboard/src/components/observe/InsightsTools.tsx` (~2,169 lines) — `InsightsToolsContent`: renders `ObserveFilterBar`, calls `useObserveFilters`, runs the aggregate queries and charts.
- `client/dashboard/src/pages/logs/Logs.tsx` — `LogsRoot`.
- `client/dashboard/src/components/observe/LogsTools.tsx` (~1,198 lines) — `LogsTools`: renders `ObserveFilterBar` + status filter + attribute-search control, calls `useObserveFilters` and `useAttributeSearchParams`, runs `telemetryListToolUsageTraces`.
- `client/dashboard/src/components/observe/useObserveFilters.ts` — the URL-synced filter hook.
- `client/dashboard/src/components/observe/ObserveFilterBar.tsx` — the shared filter bar.
- `client/dashboard/src/components/observe/ObserveTabNav.tsx` — latent tools/mcp sub-nav (unchanged by this work).
- `client/dashboard/src/pages/logs/LogFilterBar.tsx`, `log-filter-url.ts`, `log-filter-types.ts`, `LogDetailSheet.tsx`, `TraceLogsList.tsx`, `use-attribute-logs-query.ts` — the logs-only pieces.
- `client/dashboard/src/routes.tsx:482-524` — the two route entries.

## Goals

1. One page presenting the aggregate ("Insights") and row-level ("Logs") lenses over a single, shared filter state.
2. Setting a shared filter once applies to both lenses; switching lenses never resets it.
3. A chart element in Insights can be clicked to jump into Logs pre-filtered to that slice.
4. `/logs` links keep working (redirect), but there is a single nav item going forward.

## Non-goals (YAGNI)

- No backend / RPC changes. Same endpoints, same payloads.
- The tools-vs-MCP sub-dimension inside Insights is untouched (`ObserveTabNav` stays as-is).
- Chart set and trace-list internals are reused as-is, not redesigned.
- Attribute-search semantics are unchanged — only relocated to the logs lens.

## Design

### Decisions (approved)

| #                 | Decision                                                                                                       |
| ----------------- | -------------------------------------------------------------------------------------------------------------- |
| Tab hierarchy     | Top-level toggle **Insights ⇄ Logs**. Tools/MCP remains a sub-dimension _inside_ Insights.                     |
| Route             | One route with `?view=insights\|logs`. `/logs` **permanently** redirects into it, preserving the query string. |
| Nav label         | Keep **"MCP & Tools"** as the single umbrella nav item. Remove the separate "Tool Logs" item.                  |
| Drill-down        | In scope for **v1**.                                                                                           |
| Visual affordance | **Segmented control** (pill toggle) in `Page.Toolbar`, right-aligned.                                          |
| Redirect posture  | **Permanent** — single nav item, no lingering "Tool Logs" entry.                                               |

### Route & URL

- Canonical route stays `/{orgSlug}/projects/{projectSlug}/insights`, title **"MCP & Tools"**.
- Add search param `view`, values `insights` (default) and `logs`. Absent/invalid → `insights`.
- `/logs` → redirect component that reads `location.search` and navigates to `/insights?view=logs&<original query string>`, preserving every param (`range`, `server`, `status`, `q`, `af`, …). Permanent: the sidebar shows only "MCP & Tools"; the "Tool Logs" nav entry is removed.

### Visual layout (segmented control in `Page.Toolbar`)

```
┌─────────────────────────────────────────────────────────────────────────┐
│  MCP & Tools                                                              │  page header (unchanged)
├─────────────────────────────────────────────────────────────────────────┤
│  [Last 7 days ▾][Server ▾][User ▾][Source ▾][Type ▾][Role ▾]  ┌────────┐ │  Page.Toolbar — SHARED bar (left)
│                                                    Insights │ Logs        │  segmented control, right-aligned
│  ····· logs only ·····                              └────────┘           │
│  [Status ▾]  [🔍  http.response.status_code != 200          ]            │  2nd row: mounts ONLY when view=logs
├─────────────────────────────────────────────────────────────────────────┤
│                                                                           │
│   view=insights → totals · time-series · target/user breakdowns          │
│   view=logs     → infinite trace list  →  LogDetailSheet on row click     │
│                                                                           │
└─────────────────────────────────────────────────────────────────────────┘
```

The "tabbed feel" comes from **chrome stability**: the page header and the shared filter row never move when you switch lenses. Only the body swaps, and (for logs) a second toolbar row animates in below the shared row. The segmented control sits in `Page.Toolbar`'s right-aligned slot — the same slot the dashboard already uses for grid/table view toggles — so it reads as a native page control, not a bespoke tab bar.

> Implementation note: confirm the exact `Page.Toolbar` API for the right-aligned slot and a secondary control row against the `page-toolbar` skill during planning. The design assumes a right slot (for the segmented control) and a conditionally-rendered secondary row (for the logs-only controls).

### Component architecture

The one substantive refactor. Today each of `InsightsTools.tsx` and `LogsTools.tsx` renders its _own_ `ObserveFilterBar` and instantiates `useObserveFilters` independently. We lift filter ownership up into a shared container so there is exactly one filter state and one toolbar.

- **`ObservePage`** (new container, in `components/observe/`):
  - Instantiates `useObserveFilters` **once**, plus the attribute-search state (`useAttributeSearchParams`), and reads the `view` param.
  - Exposes them via a small **`ObserveFiltersContext`** provider.
  - Fetches `telemetryGetToolUsageFilterOptions` **once** and provides the options through context.
  - Renders `Page.Toolbar` **once**: shared `ObserveFilterBar` + segmented control + (when `view=logs`) the status filter and attribute-search row.
  - Renders the body: `view === "logs" ? <LogsContent/> : <InsightsContent/>`.
- **`InsightsContent`** = today's `InsightsToolsContent` with its internal `ObserveFilterBar` block and local `useObserveFilters` call removed; it consumes `ObserveFiltersContext` instead. Its query definitions and charts are otherwise untouched.
- **`LogsContent`** = today's `LogsTools` with the same treatment: drop the internal filter bar / status control / attribute-search control (those move up into the container's toolbar) and the local hook calls; consume context. Its `telemetryListToolUsageTraces` infinite query, `TraceLogsList`, and `LogDetailSheet` are untouched.

This keeps the change from becoming a rewrite: the two large bodies lose only their toolbar block and their local hook wiring. Everything downstream (queries, charts, trace list, detail sheet) reads the same shared state it always did — sourced from context instead of a local hook.

Fetching `telemetryGetToolUsageFilterOptions` once (instead of once per page) is also a small correctness win: the dropdown options can no longer drift between the two lenses.

### Filter model — shared vs tab-local

- **Shared** (persist across the toggle; already identical URL params): time range, server/MCP, user, source, type, role, account-type. Setting any of these once applies to both lenses.
- **Logs-only** (`status`, and attribute-search `q`/`af`): render only when `view=logs`. When the user switches to Insights these params **remain in the URL, harmless** — the aggregate `telemetryGetToolUsage*` queries simply don't send them — and are restored when the user switches back to Logs.

### Drill-down (v1)

Clicking an Insights element sets the matching shared filter **and** flips `view=logs`, so Logs opens pre-scoped. Three mappings for v1:

1. **Time-series point** → narrow `from`/`to` to that bucket → `view=logs`.
2. **Target / tool breakdown row** → set the server/target filter for that target → `view=logs`.
3. **User row** → set the user filter → `view=logs`.

Because both lenses read the same shared filter state, "pre-filtered" requires no extra plumbing — the drill-down handler just calls the same filter setters the toolbar uses, then sets `view=logs`. Provide a subtle affordance on clickable elements (pointer cursor; a "View logs" hover/row action where a bare click would be ambiguous).

### Navigation semantics

- **View toggle** (segmented control) → `push`, so Back returns to the previous lens.
- **Drill-down** → `push`, so Back returns to the chart the user came from.
- **Plain filter edits** → keep the existing `useObserveFilters` `replace` behavior (no history spam per keystroke/selection).

### Loading / empty / error states

- The toolbar always renders regardless of view or query state.
- Each lens keeps its existing loading/empty/error handling for its own queries.
- Shared filter options are fetched once in the container; a switch between lenses does not refetch them.

## Testing

- `view` defaults to `insights` when absent or invalid.
- Toggling `Insights ⇄ Logs` preserves all shared filters (time range, server, user, source, type, role, account-type).
- Logs-only params (`status`, `q`, `af`) survive an `insights → logs` round-trip and are ignored by the aggregate queries while in Insights.
- `/logs?server=X&range=7d` redirects to `/insights?view=logs&server=X&range=7d` with the query string intact.
- Drill-down: clicking a time-series bucket sets `from`/`to` + `view=logs`; clicking a target row sets the target filter + `view=logs`; clicking a user row sets the user filter + `view=logs`.
- `telemetryGetToolUsageFilterOptions` is fetched once, not once per lens.
- View toggle and drill-down push history (Back returns to the prior lens/chart); plain filter edits replace.
- Existing Insights and Logs content behavior (charts render, trace list paginates, `LogDetailSheet` opens on row click) is unchanged after the toolbar extraction.

## Risks & mitigations

- **Large-file refactor.** `InsightsTools.tsx` (~2.2k lines) and `LogsTools.tsx` (~1.2k lines) are being restructured to give up filter ownership. Mitigation: introduce `ObserveFiltersContext` + `ObservePage` and remove only the toolbar block + local hook calls from each body; do not touch query/chart/trace internals in the same pass.
- **Double option-fetch during transition.** Until both bodies are switched to context, two `telemetryGetToolUsageFilterOptions` fetches could coexist. Mitigation: land the container + context and migrate both bodies in one change so there is a single source.
- **`Page.Toolbar` slot assumptions.** The design assumes a right slot and a secondary row. Mitigation: verify against the `page-toolbar` skill during planning; fall back to a header-level segmented control only if the toolbar API can't host it cleanly.

## Rollout

Single release. The `/logs` redirect and the removed nav item ship together with the merged page — no interim two-nav-item state.
