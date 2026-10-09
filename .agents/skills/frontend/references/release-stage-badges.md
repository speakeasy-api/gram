# Release stage badges (Preview / Beta)

Pre-GA features get a `Preview` or `Beta` badge wherever the user would otherwise mistake the feature for being GA. The same `ReleaseStageBadge` component renders on every surface, so labels never drift.

**Source of truth:** `client/dashboard/src/components/release-stage-badge.tsx` — exports `ReleaseStageBadge` and the `ReleaseStage = "preview" | "beta"` type.

**Shape:** `ReleaseStageBadge` renders its own mono, uppercase, tracked, hairline-bordered span inside a `SimpleTooltip`; it does not compose the design-system `<Badge>`. Use the component as-is rather than re-creating its classes, and if you need a new stage style, change it there.

**Stage → color** (token names are hooks, not literal semantics):

- `preview` → warning tokens (amber).
- `beta` → information tokens (Speakeasy brand blue).

> The badge variants (`neutral | destructive | information | success | warning`) are tuned for alert/feedback contexts, but the names are just hooks — `warning` here means "experimental, use with caution," not "alert." That's the intended way to reuse the palettes; don't invent new variants without design buy-in.

**Never hardcode Tailwind colors** (no `bg-violet-500`, no raw `bg-warning-softest` spans). If you find yourself reaching for raw classes for a new badge use case, that's a signal to either pick an existing variant or add one to `@/components/ui/Badge`.

## Surface 1 — sidebar nav (route-driven)

Set `stage` on the route declaration. `app-sidebar.tsx` forwards `item.stage` through `ScopeGatedTopLevelItem → NavButton` (in `nav-menu.tsx`), which renders the badge (sidebar badges have no hover tooltip). The badge auto-hides in collapsed-icon mode. Grouped items go through `CollapsibleNavItem`, which reads `item.stage` itself.

```tsx
// client/dashboard/src/routes.tsx
assistants: {
  title: "Assistants",
  url: "assistants",
  icon: "bot",
  stage: "beta", // ← sidebar pill appears automatically
  component: AssistantsRoot,
},
```

> **Gotcha**: if you introduce another sidebar wrapper that calls `NavButton` directly, you must forward `stage={item.stage}` explicitly. `app-sidebar.tsx`'s `ScopeGatedTopLevelItem` does this — copy that pattern.

## Surface 2 — page section title

Pass `stage` on the **primary** `Page.Section.Title` for the page (usually the first `Page.Section` under `Page.Body`). Don't put it on secondary section titles like "Recent Chats" — the badge labels the whole feature, not individual sections.

```tsx
<Page.Section>
  <Page.Section.Title stage="beta">Risk Overview</Page.Section.Title>
  <Page.Section.Description>…</Page.Section.Description>
</Page.Section>
```

> **Gotcha**: pages with multiple render branches (loading / empty / populated) must show the title — and therefore the `stage` — in **every** branch. Hoist the title into one shell that every branch renders, as `RiskOverviewShell` in `SecurityOverview.tsx` does, instead of repeating it per branch.

## Surface 3 — tab nav (sub-route tabs)

Sub-route tab strips use `TabbedPage` from `@/components/page-templates`. Set `stage` on the tab's `PageTab` descriptor; `TabbedPage` renders `<ReleaseStageBadge stage={tab.stage} noTooltip />` inline after the label. Do not render the badge yourself. The page templates (`TabbedPage`, `DetailPage`, and the others) also take a `stage` prop that badges the page title.

```tsx
<TabbedPage
  title="Access"
  activeTab={activeTab}
  tabs={[
    { value: "roles", label: "Roles", href: rolesHref },
    { value: "grants", label: "Grants", href: grantsHref, stage: "preview" },
  ]}
>
  {children}
</TabbedPage>
```

If you hand-roll a tab strip (prefer `TabbedPage`), put the badge inside the tab `<Link>` with `inline-flex items-center gap-2`, so the badge tracks the label without disrupting the active-tab underline.

## Surface 4 — custom page headers (pages that don't use Page.Section.Title)

Prefer `Page.Section.Title stage="…"` (Surface 2). A page that needs a custom header must still follow the editorial idiom: the area eyebrow (`<PageEyebrow />` from `@/components/page-eyebrow`, or `<Page.Eyebrow />`) above a `text-display-sm font-thin` heading. A legacy `<h1 className="text-xl font-semibold">` is a defect; fix it to this idiom when you touch it. Put the badge next to the heading in a `flex items-center gap-2` row:

```tsx
<div className="flex min-w-0 flex-col gap-1">
  <PageEyebrow />
  <div className="flex items-center gap-2">
    <h1 className="text-display-sm font-thin">AI Agent Costs</h1>
    <ReleaseStageBadge stage="preview" />
  </div>
</div>
```

`InsightsAgents.tsx` and `OrgMemory.tsx` use this pattern.

## Which surfaces does a given feature need?

- **Default**: every surface where the user encounters the feature's name. If it has a sidebar entry **and** a page heading, badge both.
- **Tab-only feature**: badge the tab. The parent route's nav entry stays clean since the parent isn't itself pre-GA.
- **Hidden behind a feature flag**: still badge the visible surfaces. The flag controls visibility; the badge communicates stage to users who can see it.

## Removing a badge (feature ships GA)

Grep for `stage="preview"`, `stage="beta"` (JSX props) and `stage: "preview"`, `stage: "beta"` (route and tab objects), and delete only the matches that belong to the feature shipping GA — other pre-GA features keep theirs:

- `routes.tsx` — remove the `stage:` field on the route entry
- `Page.Section.Title stage="…"` — drop the prop
- `TabbedPage` tab descriptors (`PageTab`) — drop the `stage` field
- Inline `<ReleaseStageBadge>` usages — delete the element and unwrap the `flex items-center gap-2` div

There's no other cleanup. The component itself stays in place for the next pre-GA feature.
