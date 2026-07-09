---
name: gram-design-system
description: >
  Component catalog and visual rules for the dashboard's internal design system
  (the "Claude Design" look — squared corners, warm ink/bone neutrals, mono
  uppercase captions, display-serif headings, hairline borders). Activate for
  any dashboard UI work: building or modifying a component, styling a page,
  adding a new page or panel, or picking which primitive to import for a
  button/badge/dialog/table/chart/empty-state.
metadata:
  relevant_files:
    - "client/dashboard/src/components/**"
    - "client/dashboard/src/pages/**"
    - "client/dashboard/src/App.css"
---

## Background

`@speakeasy-api/moonshine` was vendored into `client/dashboard/src/components/ui/moonshine` (a compatibility barrel importable as `"@/components/ui/moonshine"`). The design system is now **internal** — it is no longer an external npm package, and the "Moonshine" name refers to the vendored implementation, not a dependency to add to `package.json`. Never `import` from `"@speakeasy-api/moonshine"`; it is denied by lint (`no-restricted-imports` in `client/dashboard/.oxlintrc.json`).

There is exactly **one implementation per UI pattern**. Some patterns kept their Moonshine implementation; others were won by the shadcn-derived `components/ui/<name>` implementations during consolidation. Always import the winner below — never reintroduce the losing implementation, and never hand-roll a duplicate.

## Component catalog — what to import from where

### From `@/components/ui/moonshine`

| Component                                                                                                                                           | Notes                                                                                                                                                           |
| --------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Button`                                                                                                                                            | All buttons — never a raw `<button className="...">`.                                                                                                           |
| `Badge`                                                                                                                                             | All badges, including `ReleaseStageBadge`'s primitive.                                                                                                          |
| `Alert`                                                                                                                                             | Inline alert/callout banners.                                                                                                                                   |
| `Input`                                                                                                                                             | Plain text inputs.                                                                                                                                              |
| `Link`                                                                                                                                              | In-copy link styling (adds an icon; heavy for inline links inside prose — see the `frontend` skill's Navigation section for when to use a plain `<a>` instead). |
| `Table`, `Column`                                                                                                                                   | All dashboard data tables — see the `frontend` skill's Tables section for the declarative/compound API split.                                                   |
| `DropdownMenu`, `DropdownMenuContent`, `DropdownMenuItem`, `DropdownMenuLabel`, `DropdownMenuGroup`, `DropdownMenuTrigger`, `DropdownMenuSeparator` | All dropdown menus.                                                                                                                                             |
| `Stack`, `Grid`                                                                                                                                     | Responsive flex/grid layout primitives.                                                                                                                         |
| `Icon`                                                                                                                                              | The icon set (`IconName` for the type union).                                                                                                                   |

### From `@/components/ui/<name>` (internal implementation won)

| Component                                      | File                                                              | Notes                                                                                                                |
| ---------------------------------------------- | ----------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- |
| `Dialog` family                                | `dialog.tsx`                                                      | Requires `DialogTitle` (+ optional `DialogDescription`) inside `DialogContent`.                                      |
| `Tooltip` family                               | `tooltip.tsx`                                                     | Global `TooltipProvider` lives in `App.tsx` — never nest another one.                                                |
| `Select`                                       | `select.tsx`                                                      |                                                                                                                      |
| `Tabs`                                         | `tabs.tsx`                                                        |                                                                                                                      |
| `Sheet`                                        | `sheet.tsx`                                                       |                                                                                                                      |
| `Skeleton`, `SkeletonTable`                    | `skeleton.tsx`                                                    | Use instead of a raw `animate-pulse` div — see Prohibitions.                                                         |
| `Separator`                                    | `separator.tsx`                                                   |                                                                                                                      |
| `Card`, `CardMini`, `DashboardCard`, `DotCard` | `card.tsx`, `card-mini.tsx`, `dashboard-card.tsx`, `dot-card.tsx` | Pick the variant that matches the surface; don't hand-roll a bordered `<div>`.                                       |
| `Heading`                                      | `heading.tsx`                                                     | Page/section titles. `h1`–`h3` render in the display serif (Tobias); `h4`–`h6` stay in the interface sans (Diatype). |
| `Type`                                         | `type.tsx`                                                        | Body text primitive — prefer over raw `<p>`/`<span>` for anything that should track the type scale.                  |

Also in `components/ui/` and available as needed: `Accordion`, `Avatar`, `Checkbox`, `Combobox`, `Command`, `ContextMenu`, `CopyButton`, `Editable`, `Field`, `HoverCard`, `InputGroup`, `Label`, `McpIcon`, `MoreActions`, `MultiSelect`, `Popover`, `PrivateInput`, `RadioGroup`, `SearchBar`, `SegmentedControl`, `Slider`, `Sonner` (toasts), `Spinner`, `Switch`, `Textarea`, `ToggleGroup`, `Toolbar` (`Page.Toolbar` — see the `page-toolbar` skill), `ViewToggle`, `XyFade`, `Sidebar`.

### Core components (new or landing)

These live under `components/ui/` alongside the rest of the catalog. If one you need isn't merged yet, check for it before hand-rolling the pattern — it's likely mid-migration on another branch.

| Component                      | One-line usage                                                                                                    |
| ------------------------------ | ----------------------------------------------------------------------------------------------------------------- |
| `ConfirmDialog` / `useConfirm` | Confirmation prompts. Replaces `window.confirm` everywhere — see Prohibitions.                                    |
| `StatTile`                     | A single labeled stat/metric tile (value + caption), used in stat rows and summary cards.                         |
| `StatusDot`                    | A small colored dot indicating status (online/offline, healthy/degraded, etc.).                                   |
| `Progress` / `UsageMeter`      | A horizontal usage/progress bar (e.g. quota consumption, storage usage).                                          |
| `IdentityCell`                 | A user/agent identity cell — avatar + name (+ optional secondary line) for table rows and lists.                  |
| `DetailList`                   | A label/value key-value list for detail panes (settings summaries, metadata panels).                              |
| `InlineEmptyState`             | A compact empty state for a section or panel (not a full page) — smaller than the page-level empty state pattern. |
| `LoadMoreFooter`               | A sibling footer below a list/table with a "Load more" control and result count.                                  |
| `Kbd`                          | Renders a keyboard shortcut/key hint (wraps the Moonshine `Key`/`KeyHint` primitives).                            |

### Chart system (`components/chart/`)

| File / export    | One-line usage                                                                                                  |
| ---------------- | --------------------------------------------------------------------------------------------------------------- |
| `chart-theme.ts` | Shared chart color tokens/scales — pull chart colors from here, never hardcode hex values in a chart component. |
| `Timeseries`     | Time-series line/area chart (tool calls over time, cost over time, etc.).                                       |
| `Sparkline`      | Compact inline trend chart for stat tiles and table cells.                                                      |
| `RankedBar`      | Horizontal ranked bar list (top tools, top agents, top costs).                                                  |

If a chart need doesn't fit these, check `ChartCard`, `MetricCard`, and `chartUtils.ts` in the same directory before building a one-off chart wrapper.

## Visual rules ("Claude Design")

- **Tokens over hardcoded colors.** Use design-token utility classes (`bg-background`, `text-foreground`, `text-muted-foreground`, `border-border`, `bg-lang-*`, etc.) — never a hardcoded hex value or a raw Tailwind palette color (`bg-neutral-100`, `border-gray-200`, `text-gray-500`, `#3b82f6`, …).
- **Squared corners.** The theme collapses the entire Tailwind radius scale to `0px` (`--radius-xs` through `--radius-4xl` in `components/ui/moonshine/global.css`). Never add a `rounded-*` utility to work around this — the only sanctioned exception is `rounded-full`, reserved for dots and avatars (circles).
- **Mono uppercase captions.** Captions, eyebrows, and labels (table headers, tags, small metadata text) render in the mono font, uppercase, letter-tracked — never plain sans for these roles.
- **Display-serif headings.** Page and section titles go through the `Heading` component. `h1`–`h3` render in the display serif (`font-display`, Tobias); `h4`–`h6` stay in the interface sans (`font-sans`, Diatype). Don't hand-roll `<h1 className="text-xl font-semibold">` — see the `frontend` skill's Release Stage Badges section for the one place this pattern still lingers and is slated for migration.
- **Language palette as identifier colors.** `--color-lang-{typescript,javascript,python,go,ruby,php,java,csharp,rust}` (exposed as `bg-lang-*`/`text-lang-*`) is reserved for language/tech identifiers (badges, icons, code-source tags) — not for general decorative accents.
- **Hairline borders, no shadows.** Separate surfaces with a 1px `border-border` hairline, not a box-shadow. The system is flat — don't add elevation shadows to cards, dialogs, or dropdowns to imply depth.

## Storybook-first workflow

Every component in `components/ui/` must have a colocated `<name>.stories.tsx`. Run Storybook locally with `mise start:storybook` (port 6007).

Workflow for any new or modified shared component:

1. Add or modify the component in `components/ui/` (or `components/chart/` for chart primitives).
2. Add or update its `.stories.tsx` in the same directory, covering the meaningful variants/states.
3. Run `mise start:storybook` and verify the component renders correctly across its states before wiring it into a page.

## Prohibitions

- **No hand-rolled duplicates of catalog components.** If a pattern above exists, use it — don't recreate a button, badge, dialog, table, tabs, select, etc. from scratch.
- **No `window.confirm`.** Use `ConfirmDialog` or the `useConfirm` hook instead.
- **No raw `animate-pulse` divs.** Use `Skeleton` (or `SkeletonTable` for tabular loading states) from `@/components/ui/skeleton`.
- **No new `rounded-*` corners.** The only exception is `rounded-full` for dots/avatars.
- **No hardcoded hex or Tailwind-palette colors.** Use design tokens (see Visual rules above). This is enforced for the Moonshine import path by lint; it is not (yet) statically enforced for arbitrary color classes — catch it in review.

## Cross-references

- `frontend` — general React/dashboard conventions (component structure, performance patterns, navigation, copy rules); its Styling section points here for the full catalog.
- `page-toolbar` — the `Page.Toolbar` compound component for list-page search/filter/sort/view controls.
- `vercel-react-best-practices` — React performance patterns to apply when building new components.
