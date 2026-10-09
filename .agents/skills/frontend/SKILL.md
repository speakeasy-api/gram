---
name: frontend
description: Use when building, designing, redesigning, or reviewing any UI in the dashboard React frontend (client/dashboard, including the inlined Speakeasy Elements code) — a new page, tab, card, table, form, dialog, sheet, empty or error state, settings section, or UI copy — and when addressing design feedback or Gutternote comments on a preview. Triggers: "build a page for", "add UI for", "make this look better", "redesign", "the UX is confusing", "layout shift", "overflow", "add a tab", "dashboard component".
metadata:
  relevant_files:
    - "client/dashboard/**"
---

# Dashboard frontend

Good UI does not come from a better first prompt. It comes from running a short loop several times. Most dashboard UI that needs rework failed the same way: it was built from the backend model outward and never judged on screen. Each pattern below came up in several recent PRs and needed follow-up PRs to fix. Code hygiene was rarely the problem.

## Designing UI (read first)

### Size the loop to the change

| Change                                                                              | Do                                                  |
| ----------------------------------------------------------------------------------- | --------------------------------------------------- |
| One label or string                                                                 | Law 1 check; view it once in the running app        |
| Bug fix or style fix on an existing surface                                         | Step 4, only for the states you touched             |
| New card, section, column, dialog, or state; rework of an existing table or section | Steps 1, 2 (repo precedent only), 4, 5              |
| New page, tab, nav item, or flow; anything that adds a concept to the UI            | All steps; stop after step 3 for the user to choose |

Working with no user to choose? Pick your recommended direction, put the other two in the PR description, and go on. If the request names a new page but the job belongs on an existing one, make the existing page your recommended direction and say why.

### The loop

1. **Frame the job.** Before any code, write this in your reply:

   ```
   Who:       <role> opening this page, and what just happened that sent them here
   Job:       <one sentence, no table, API, or field names>
   Flow:      1. … 2. … 3. …   (steps to the goal; fewer is better)
   Words:     <backend term> → <user word>   (one line per term the UI could leak)
   Lives on:  <page>, because that is where the user changes it
   Scope:     what this applies to (org / project / server) and how the user sees and picks it
   Next:      for each state and each row type, the one thing the user does next
   ```

   `principal` → "person or agent", `toolset` → "MCP server", `grain` → drop it. Put information where the user _changes_ it, not where it loosely relates.

2. **Find two precedents.** One page in our app and one outside product that already solve this job. Name the exact detail you will copy — "the Polar sidebar's white active state and track", not "like Polar". Start from the [reference shelf](#reference-shelf). Precedents may predate these rules: copy the pattern, not its violations.
3. **Show three directions.** ASCII sketches before code or data-model changes. Directions must differ in _structure_ (where it lives, what pattern, what the user does first), not in styling. At least one removes a step, a field, or a tab. Recommend one and say why.
4. **Build on real data, then critique.** Seed realistic volume and edge cases (`gram-demo-seed` skill): long names, many rows, zero rows, failures. Screenshot each state with `mise run playwright` (`gram-playwright-cli` skill): loading, empty, error, full, longest real value, 400px wide. Then critique — see [How to critique](#how-to-critique). Fix, re-screenshot, repeat until a round finds nothing. Expect several rounds; strong surfaces here took dozens. No browser? Critique the code against the checklist, list the screenshots you would take, and say they are not taken.
5. **Subtract, then systemise.** Remove at least one thing you built: a repeated heading, an intro that restates the title, a control nobody needs, a column nobody reads. If you built the same pattern twice, make it a component.
6. **Ship and close the feedback loop.** Open the PR with Gutternote review steps (`pull-request` skill). Reviewers comment on the preview with [Gutternote](#gutternote-feedback); address the comments through its MCP and repeat step 4 on what changed.

### Three laws

**1. Speak the user's words, not the schema.** If a label, header, badge, legend, tooltip, or toast names a table, enum, scope, protocol field, slug, or ID, rewrite it.

| Wrote                                 | Ship                                        |
| ------------------------------------- | ------------------------------------------- |
| `principal`                           | person / agent                              |
| `tool:connect, selector=*`            | Can connect to all tools                    |
| Re-publish                            | Sync                                        |
| `organization:enterprise_trial_armed` | Enterprise trial started                    |
| Token Endpoint Authentication Methods | (behind "Technical details")                |
| `{"name":"not_found", …}`             | Server not found. It may have been removed. |

**2. Design the outcome; records are how you get there.** Shape the page around the user's decision, not the API's records. Count the steps to the goal; every row should lead to a next action.

| Record-shaped                                  | Outcome-shaped                                         |
| ---------------------------------------------- | ------------------------------------------------------ |
| Policy form with block / flag options          | One toggle that creates or deletes the policy          |
| Add servers to a plugin, then press Re-publish | Servers distribute automatically                       |
| Audit log of access denials                    | A Grant button on each row                             |
| Save, then find out it's broken                | Save stays locked until the connection is verified     |
| Three API records → three panels               | One panel, because the admin thinks of it as one thing |
| One row per grant, so roles repeat             | One row per role                                       |
| Widgets get their own tab next to Dashboards   | Widgets are a step inside building a dashboard         |

A primary feature gets a sidebar item, not a sub-tab of another page.

**3. Reuse what we have; borrow what we don't.** Use the [reference shelf](#reference-shelf) and [Building a page](#building-a-page). A fourth drawer slightly different from the other three is a defect, as are a page-level `.css` file, a custom breakpoint, a hand-rolled card, or a bespoke heading. For anything we lack, copy a named detail from a named product.

### Choosing a pattern

Pick the container by what the user is doing, then use the one component we have for it.

| The user is…                                   | Use                                                                                                                                                                                                                                                                  | Not                                                          |
| ---------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------ |
| Browsing or comparing many of a thing          | List page (`ResourceListPage`) with `Page.Toolbar` + `Table`                                                                                                                                                                                                         | Cards, when there are more than ~8                           |
| Looking at a few rich things                   | Card grid                                                                                                                                                                                                                                                            | A table with one wide column                                 |
| Understanding or configuring one thing         | Detail page (`DetailPage`), sections in order of use                                                                                                                                                                                                                 | A tab per API sub-resource                                   |
| Doing a short task without losing their place  | `Sheet` (side panel)                                                                                                                                                                                                                                                 | A new page or a nested dialog                                |
| Confirming or making one decision              | `ConfirmDialog` / `Dialog`                                                                                                                                                                                                                                           | A sheet, an alert, `window.confirm`                          |
| Following a multi-step setup                   | `WizardPage`, steps in user words                                                                                                                                                                                                                                    | One long form                                                |
| Editing one field in place                     | Inline edit                                                                                                                                                                                                                                                          | A dialog for one field                                       |
| Changing settings in a panel                   | One save model per panel: everything applies at once (simple, reversible toggles), or everything waits for Save (anything that changes production behavior — then a checkbox or "Enable" field inside the form, not a switch). Warn before discarding unsaved edits. | A switch that saves at once beside fields that wait for Save |
| Needing rare or technical detail               | `Collapsible` "Technical details" / "Advanced", closed                                                                                                                                                                                                               | Showing it by default                                        |
| Seeing a short hint                            | Tooltip — never the only place a next step lives                                                                                                                                                                                                                     | A paragraph above the control                                |
| Being told something happened                  | Toast for done/failed; inline `Alert` for what they must act on                                                                                                                                                                                                      | A toast for an error they must fix                           |
| Switching between different things on one page | Page tabs (`TabbedPage`) — only for genuinely different jobs                                                                                                                                                                                                         | Tabs that split one job into pieces                          |
| Deleting                                       | `DangerSettingsSection` at the end of the detail page + `ConfirmDialog`                                                                                                                                                                                              | A one-click overflow-menu item                               |

### Visual hierarchy

The eye should land on the answer first. In each block:

- **One primary action per view.** One solid button; everything else is secondary or tertiary. Two solid buttons means you have not decided what the page is for.
- **Weight follows importance.** The state or number the user came for is the biggest thing. Labels are `text-eyebrow`; metadata is muted. If everything is bold, nothing is.
- **Scan order.** Identity on the left, status and actions on the right. The answer sits top-left; diagnostics and history sit below the primary task.
- **Group by space, not boxes.** Related items share a tight gap; groups are separated by a larger gap or a hairline. Never a card inside a card.
- **Color means something.** Status uses colored text, a dot, or a badge on a neutral surface. Nothing is colored for decoration.
- **Quiet when healthy.** The normal state shows no badge, no warning, no "No issues" filler. Only exceptions draw the eye.
- **Room to breathe.** Generous padding, consistent gaps, one main thing per block. Pixels are free; do not cram to fit above the fold.
- **Same thing, same look.** A status, an empty state, a filter bar looks identical on every page. If yours differs, you are wrong or the shared one needs changing — change it in one place.

### Reference shelf

| Need                  | In our app                        | Outside — copy this exact detail                |
| --------------------- | --------------------------------- | ----------------------------------------------- |
| Search/filter a list  | `Page.Toolbar` (`page-toolbar`)   | Vercel log explorer: filters as chips, one row  |
| Do a task in a panel  | `Sheet`                           | Granola: assistant docked beside the work       |
| Show numbers          | `StatRow` / `MetricCard`          | Stripe: one number, one delta, one label        |
| Nothing yet           | `InlineEmptyState` / `EmptyState` | Linear: empty state names the first action      |
| Confirm               | `ConfirmDialog`                   | GitHub: type-to-confirm only for irreversible   |
| Roles and access      | Team Access page                  | GitHub: roles as named bundles, not scope lists |
| Build a rule or query | —                                 | PostHog rule builder; Datadog one-row query bar |
| Navigation            | App sidebar                       | Polar: white active state and track             |

### How to critique

Critique is where taste comes from; do not skip it or do it in your head.

1. Open each screenshot with the Read tool and look at it. Do not critique from the code.
2. Better: hand the screenshots to a fresh subagent with no knowledge of how you built it, with this prompt:

   ```
   Critique these screenshots of <page> like a picky design lead. The user's job is: <job>.
   Look for: internal jargon; CRUD where one action would do; technical detail before state;
   layout shift between states; overflow from long values; dead-end badges or errors;
   repeated information; unrelated things on one page; cramped spacing; competing primary
   actions; anything that looks different from the rest of the app.
   Return one line per issue: <element> → <exact fix>. Most important first.
   ```

3. Check the result against the [critique checklist](#critique-checklist). Fix, re-screenshot, and run it again with a new subagent.

### Critique checklist

Each line is a sign the feature still describes the backend, not the outcome. Implementation for each lives in `references/ui-states.md`.

**Words**

- [ ] No table, enum, scope, protocol field, slug, or ID in primary UI. Technical detail sits behind "Technical details".
- [ ] The header says what the user gets ("See what your team's agents do"), not how it works ("Configure PreToolUse hook ingestion endpoints").
- [ ] Errors say what happened in plain words and what to do next. Server text is never the main message.
- [ ] No ID or ID fragment in visible text — not `agt_01J9…`, not "Person 3f9a12bc", not "Unnamed agent 9f3a2c". Show the name; when there is none, a plain noun ("Unnamed agent", "Former member") with the ID in a tooltip or copy button, and report missing names as a backend gap. Dates read "14 Aug" or "2h ago".
- [ ] Each idea is explained once, next to where it applies. No intro that repeats the heading below it.
- [ ] Sentence case for titles, labels, buttons, and alert titles.
- [ ] Logic the user must know is spelled out (do these conditions combine with AND or OR?).
- [ ] Every count, chart, and history names its time window ("in the last 7 days"), and one surface uses one window. "42 refused" alone means nothing.
- [ ] One word per concept everywhere — tab, heading, button, toast ("cap" or "budget", not both).

**Outcome**

- [ ] I can say the user's job in one sentence without naming a table.
- [ ] The primary task is first on the page; diagnostics and history come after it.
- [ ] Every card reads: state → what it means → next action → details.
- [ ] Every warning, error, and blocked state names the next step as a button or link — or the person to ask. Not prose; not only a tooltip.
- [ ] Empty state leads with the outcome and one first action, not "No records" + Create.
- [ ] No control that does nothing. Rare settings sit under "Advanced".
- [ ] One page, one topic. Unrelated concepts (keys, branding, billing, sync) live on their own pages.
- [ ] The main outcome takes the fewest possible steps.
- [ ] Every table row leads somewhere — for every role that can see it, not only admins: the name links to its detail or activity, or the row has an action that fixes what it shows. Use one action label for every row. A log the user can only read is a dead end.
- [ ] Evidence comes before a destructive action: show what will be affected, then the button that breaks it.
- [ ] State-changing actions (publish, deprecate, pause) sit where the state is shown, not only in a Settings tab.
- [ ] Settings show who last changed them and when ("Changed by Priya Raman · 3 Oct").
- [ ] One detail surface per object. Extending an existing page beats adding a sheet or page that duplicates it.
- [ ] Permissions are handled the same way everywhere for an action: hide write controls the user cannot use, and say once, at the section, who can change it ("Only org admins can change this").
- [ ] A setting that needs a number shows the data to choose it next to the input ("Busiest caller peaked at 6 calls a second this week").
- [ ] Important state shows where users already look — the list row or overview — not only on the settings tab that changes it.

**Look**

- [ ] One primary action per view, in the same slot in every state ("Set cap" ↔ "Edit cap"); the eye lands on the answer first.
- [ ] One fact, one place — no number shown twice in a tile and a caption.
- [ ] When you extend an existing page, subtract too: a table carries only the columns its decision needs.
- [ ] Color is never the only signal; a dot or tint also has text or a label.
- [ ] Nothing jumps between loading, empty, filtered, and loaded. Every region holds its loaded height from first paint: toolbars and child sections render while the page loads, and empty states are the same height as their skeleton.
- [ ] The longest real value fits: truncated with the full value on hover, badges wrap, nothing spills out of its card.
- [ ] Columns sized to content: the name takes the free space, numbers right-aligned, no ID nobody reads.
- [ ] Controls stay compact: at most three pinned filters; a query builder does not push results below the fold.
- [ ] Room to breathe; no card inside a card (options inside a section are plain radio rows, not bordered radio cards); healthy states are quiet.
- [ ] Every pattern matches the rest of the app (status, empty state, filter bar, dialog, heading).

**Ship**

- [ ] Every tab, route, and empty/error branch was clicked in the running app.
- [ ] I compared three directions and two references, and removed at least one thing I first built.
- [ ] Screenshots of each affected state are on the PR, and the PR has Gutternote review steps.

### Gutternote feedback

Preview apps for UI PRs load the [Gutternote](https://gutternote.com) widget, so reviewers comment directly on the page. Install its MCP once so your coding agent can read those comments and address them.

Claude Code:

```bash
claude plugin marketplace add gutternote/gutternote-plugins
claude plugin install gutternote@gutternote-plugins
```

Then run `/mcp`, choose `gutternote`, and approve the connection in your browser.

Codex:

```bash
codex plugin marketplace add gutternote/gutternote-plugins
codex plugin add gutternote@gutternote-plugins
codex mcp login gutternote
```

When Gutternote comments arrive on your PR's preview: fetch them with the MCP, fix each one, re-run step 4 on the affected screens, push, and reply on the comment with what changed. If the MCP is not installed, tell the user and give them the commands above rather than skipping the feedback.

## Building a page

**First choice: a page template.** Do not hand-roll the `Page` frame. Pick the template that matches the page's shape from `@/components/page-templates` and fill in data — it owns the frame, breadcrumbs, scope gate, the single header, and the loading/empty branches (which is what stops the "three-branch header" duplication). Full recipes: `client/dashboard/src/components/page-templates/README.md`.

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
  const q = useListEnvironments(); // SDK query hook; runs only after the page gate passes
  const rows = q.data?.environments ?? [];
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
      isEmpty={!q.isError && rows.length === 0} // unfiltered rows: isEmpty hides the toolbar
      empty={{
        icon: "blocks",
        heading: "No environments yet",
        action: newButton,
      }}
    >
      {q.isError ? (
        <InlineEmptyState
          icon="triangle-alert"
          heading="Couldn't load environments"
          action={<Button onClick={() => q.refetch()}>Retry</Button>}
        />
      ) : (
        <Table columns={columns} data={rows} rowKey={(r) => r.id} />
      )}
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
- Delete and other destructive actions → a `DangerSettingsSection` at the end of the detail page (its destructive tint is the one allowed wash), gated by the write scope; never a one-click overflow-menu item. On success, invalidate the list query and go to the list route.
- Confirm-before-acting → `ConfirmDialog` from `@/components/ui/ConfirmDialog`. It locks itself while `isPending` (pass `pendingLabel`, e.g. "Deleting…", to name the action in progress), shows an inline `error` the user must read, and can show a pre-flight `impact` list. Never hand-roll a confirm from raw `Dialog` parts. Show a specific refusal inline in its `error` (a node, so it can link to the fix) by branching on `error instanceof GramError && error.statusCode === 409` (`@gram/client/models/errors/gramerror.js`); toast everything else with `handleError`.
- Loading → content-shaped skeletons (`SkeletonTable`, geometry-matched rows), never a lone spinner or a premature empty state.
- Cards white (`bg-card`), page gutter gray, hairline borders, no shadows/gradients/washes, square corners — per the styling rules below.

A page that renders its own `<h1>` instead of this pattern is a defect; if a custom header is unavoidable, it must still render `<PageEyebrow />` + `text-display-sm font-thin`.

## Design-system inventory (consolidated — don't import the removed ones)

The primitive shelf was consolidated; these imports are gone or moved. Reaching for a removed one is a defect:

- **`MetricCard`** — one primitive only: `@/components/ui/MetricCard` (`label`/`value`/`tone`). The analytics tile that formats numbers/thresholds/deltas is **`StatTile`** (`@/components/chart/stat-tile`, `StatTile`/`StatTileGroup`) — _not_ a second `MetricCard`.
- **Password inputs** — no `PrivateInput`; use `<Input type="password" reveal />` (the `reveal` prop adds the show/hide eye toggle).
- **`DashboardCard`** — now `Card.Dashboard` (`title`/`action`/`tooltip` + children).
- **`ToggleButton`** — imported from `@/components/ui/SegmentedControl` (re-homed); use `SegmentedControl` for a full option group.
- **`Modal` / `IconButton`** — removed. Use `Dialog` for modals; `Button` with the `icon` prop for icon-only buttons.
- **Detail-page shared bones** live in `@/components/detail/`: `SettingsSection`/`DangerSettingsSection` (`settings-section`, also re-exported from the `page-templates` barrel) and `DetailSidebarNav`/`DetailSidebarInfoLabel` (`detail-sidebar-nav`) — not under `pages/mcp/...` and not `Mcp`-prefixed.

## Styling and Design System

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

## Editing copy

- **Always use American (US) English spelling in user-visible copy.** Every string the user reads — labels, headings, buttons, tooltips, placeholders, empty states, toasts, error messages — uses American spelling, per the Speakeasy brand style guide. Highest-frequency cases: `color` not `colour`, `canceled`/`canceling` not `cancelled`/`cancelling`, `behavior` not `behaviour`, `license` (noun and verb) not `licence`, `catalog` not `catalogue`, `gray` not `grey`, `center` not `centre`, `analyze` not `analyse`, and `-ize`/`-ization` endings (`organize`, `customize`, `authorization`, `synchronization`) not `-ise`/`-isation`. This applies only to what the user reads — leave code identifiers, API field names, and third-party tokens alone even when they use British spelling (e.g. an upstream `colour` field stays `colour`; only the visible label is Americanized).
- **Preserve dynamic tokens.** Page subtext often interpolates state like `{rangeLabel}`, `{periodUsage.credits}`, or `{projectName}`. When rewording copy that contains a token, keep the token in place — replace it with the literal current value (e.g. "the last 30 days") only when the data fetch itself is locked to that value. Otherwise the copy starts lying as soon as the user changes a filter.
- **Don't fight the Tailwind class sorter.** Prettier's `prettier-plugin-tailwindcss` reorders classes on save. Write classes in any order; the formatter will normalize them and the diff stays clean across the codebase.
- **AI context strings shadow user-visible names.** When renaming a chart or card (e.g. "Most Used LLM Clients" → "Most Used Agents"), search for the old name in nearby `contextInfo=` / `suggestions=` props passed to `ExploreWithAI` / `InsightsConfig`. Those strings are sent to the LLM as analytical context; if they drift from the visible label, the AI assistant talks about a card the user can't see.

## Code rules

### Verification Commands

Use `aube` package scripts for frontend checks. From the repo root, prefer `aube run -F <package> <script>` so commands run against the right frontend package without `cd`. Do not run `npm exec`, `npx`, bare `vitest`, bare `eslint`, bare `oxfmt`, or bare `tsc` unless you are debugging the package script itself.

| Need                | Dashboard                           | Whole workspace                                 |
| ------------------- | ----------------------------------- | ----------------------------------------------- |
| Package lint gate   | `aube run -F dashboard lint`        | `aube run lint`                                 |
| Type-check only     | `aube run -F dashboard type-check`  | `aube run type-check`                           |
| Tests once          | `aube run -F dashboard test`        | Run the touched package's `aube run test`       |
| Tests in watch mode | `aube run -F dashboard test:watch`  | Run the touched package's `aube run test:watch` |
| ESLint only         | `aube run -F dashboard lint:eslint` | Run the touched package's script                |
| Format check only   | `aube run -F dashboard lint:format` | Run the touched package's script                |

For small edits, run the narrowest package script that proves the change. For shared or cross-package frontend changes, run the root `aube run lint` and `aube run type-check` scripts.

### General Guidelines

- Name the actual state or behavior in identifiers and UI copy, not relative labels such as `legacy` or `modern`. For example, use `agentIdentityIsNotConfigured` for an assistant without an agent identity. Choose the name from the actual condition. Preserve externally contracted values, including metric labels used by queries or alerts, unless the change includes a compatibility plan.

- Use the `aube` package manager
- When interacting with the server, use the `@gram/client` package (this is an alias to `client/dashboard/src/sdk/src/...`)
- The document `client/dashboard/src/sdk/REACT_QUERY.md` is very helpful for understanding how to use React Query hooks that come with the SDK.
- For data fetching and server state, use `@tanstack/react-query` instead of manual `useEffect`/`useState` patterns
- When invalidating React Query caches after mutations, invalidate ALL relevant query keys — not just the most specific one. Different hooks may use different query key prefixes for the same data (e.g., `queryKeyInstance` vs `toolsets.getBySlug`). Use broad invalidation helpers like `invalidateAllToolset(queryClient)` to ensure all consumers refresh.

### References

Read the matching file before writing that kind of code:

| Writing                                                                            | Read                                 |
| ---------------------------------------------------------------------------------- | ------------------------------------ |
| Any component; shared className/JSX; ternaries; page headers; empty states         | `references/code-structure.md`       |
| A table, its filters, pagination, grouped or expandable rows                       | `references/tables.md`               |
| Telemetry/observability fetching, search, filtering, keyboard navigation           | `references/data-and-performance.md` |
| Tooltips, internal/external links, drawers, dialogs                                | `references/overlays-and-links.md`   |
| A Preview/Beta badge, or removing one at GA                                        | `references/release-stage-badges.md` |
| Errors, empty/loading states, IDs, dates, truncation, sizing (checklist mechanics) | `references/ui-states.md`            |
