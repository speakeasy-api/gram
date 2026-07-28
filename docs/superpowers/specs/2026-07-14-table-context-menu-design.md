# Right-click context menus for all entries with per-entry actions

Date: 2026-07-14
Status: Approved

## Goal

Every table row, card, and list entry in the Gram dashboard that exposes per-entry
actions (a "⋯" kebab menu or equivalent) also opens the same actions on
right-click. This finishes the initiative already live for source, environment,
custom-tool, prompt, resource, and assistant cards (`CardContextMenu`) and for
session rows (hand-wired `ContextMenu`).

## Non-goals

- Entries with no per-entry options (catalog, costs, top-users, billing tables)
  stay untouched.
- No new actions are invented; the right-click menu mirrors what the kebab
  already offers.
- Page-level `MoreActions` (detail-page headers, `page-layout.tsx`,
  `card-mini.tsx`) are out of scope — they are not entries.

## Architecture

### 1. Moonshine change (PR in speakeasy-api/moonshine)

Moonshine's `Table` owns each `<tr>` via an internal `RowContainer`
(`src/components/Table/index.tsx`) and exposes only `onRowClick`. Two changes:

- `RowContainer` forwards refs and spreads extra props onto the `<tr>`, so it
  can serve as a Radix `asChild` target.
- `Table` gains one prop, default identity:

  ```ts
  renderRow?: (row: T, rowElement: React.ReactElement) => React.ReactNode
  ```

  Consumers wrap each data row, e.g. in
  `<ContextMenu><ContextMenuTrigger asChild>{rowElement}</ContextMenuTrigger>…</ContextMenu>`.
  In `RowExpandable` the wrapper applies to the `<tr>` only, not the
  expanded-content panel; rows inside `RowGroup` get the same treatment.

A narrower `onRowContextMenu` callback was rejected because Radix context menus
must own their trigger element to position at the pointer; the render-prop is
the shape that composes. Ship as a minor release.

### 2. Dashboard shared component (gram repo)

New `TableRowContextMenu` in `client/dashboard/src/components/`, sibling of
`CardContextMenu` (which wraps children in a `<div>` — invalid inside
`<tbody>`):

- Takes the same `Action[]` from `ui/more-actions.tsx`.
- Renders `ContextMenuTrigger asChild` directly around the row element.
- No-ops (renders the row unwrapped) when `actions` is empty.

Used by DotTable rows (`DotRow` already forwards `onContextMenu` and was
designed to back `ContextMenuTrigger asChild`) and as the `renderRow` wrapper
for moonshine tables.

### 3. Wiring — one `Action[]`, two menus

For each target, build the `Action[]` once and feed it to both the visible
kebab (`MoreActions` or existing `DropdownMenu`) and the context menu.

- Rows whose kebab is a raw `DropdownMenu` with a flat item list are refactored
  to the shared `Action[]` + `MoreActions` pattern.
- Complex menus that don't map to a flat `Action[]` (e.g. Team role management
  radio groups) keep their dropdown and get a hand-wired matching
  `ContextMenuContent`, like `SessionRow` does today.

### Targets

DotTable rows:

- `src/components/sources/SourceTableRow.tsx` (Sources)
- `src/pages/remote-identity-providers/tabs/client/McpServersTab.tsx`
- `src/pages/remote-identity-providers/tabs/client/SessionsTab.tsx`
- `src/pages/remote-identity-providers/tabs/issuer/ClientsTab.tsx`

Moonshine tables (via new `renderRow`):

- `src/pages/deployments/Deployments.tsx`
- `src/pages/team/Team.tsx`
- `src/pages/access/RolesTab.tsx`
- `src/pages/security/ExclusionsTab.tsx`
- `src/pages/security/PolicyCenter.tsx`
- `src/pages/triggers/Triggers.tsx`
- `src/components/shadow-mcp/ShadowMCPInventoryTable.tsx`

Cards/lists (via existing `CardContextMenu`):

- `src/pages/org/OrgHome.tsx` (project cards)
- `src/pages/plugins/PluginCard.tsx`
- `src/components/sources/Sources.tsx` (grid)
- `src/components/tool-list/ToolList.tsx`
- `src/pages/sources/SourceToolActions.tsx`

Also: `src/pages/chatLogs/ChatLogsTable.tsx` (single per-row Delete action) —
wire with `TableRowContextMenu` or `CardContextMenu` according to whichever
table system it uses, confirmed during implementation.

## Error handling

- Empty or permission-gated action lists render the entry unwrapped (no dead
  right-click menu); disabled actions render disabled, matching kebab state.
- Destructive actions use the `destructive` item variant and continue to route
  through their existing confirm dialogs — the context menu only triggers the
  same `onClick`.

## Testing

- Dashboard: tests for `TableRowContextMenu` following
  `card-context-menu.test.tsx` (opens on contextmenu, fires action, destructive
  variant, empty no-op). `pnpm -F dashboard type-check` and build must pass.
- Moonshine: Table story + test covering `renderRow` (wrapper renders, row
  still clickable/expandable).
- Manual spot-check of each wired page via the dev server.

## Rollout

Two PRs:

1. Moonshine: `renderRow` prop + ref-forwarding `RowContainer`, minor release.
2. Gram: bump `@speakeasy-api/moonshine`, add `TableRowContextMenu`, wire all
   targets. The DotTable/card work doesn't depend on the moonshine release but
   ships in the same PR for consistent behavior.
