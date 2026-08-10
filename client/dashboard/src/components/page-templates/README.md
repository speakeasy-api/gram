# Page templates — the paint-by-numbers layer

Every dashboard page is one of a small number of shapes. Instead of hand-rolling
the `Page` frame, breadcrumbs, scope gate, header, and loading/empty/error
branches on each page (which is how they drifted apart), **pick the template
that matches your page's shape and fill in the data.** The template owns the
chrome; you own the content.

> If you're building a page and reach past a template into raw `<Page>` /
> `<h1>` / `border-dashed` markup, that's a signal the template is missing a
> prop — add the prop, don't fork the layout.

## Which template?

| Your page is…                                                        | Use                |
| -------------------------------------------------------------------- | ------------------ |
| a collection: search/filter + a table or card grid + empty state     | `ResourceListPage` |
| one entity: a hero + sections (routed subpages, or a scroll)         | `DetailPage`       |
| tabs that are _different resources_ (not one entity's sections)      | `TabbedPage`       |
| a single create/edit form                                            | `FormPage`         |
| stacked titled config sections (or a prose/tool column)              | `SettingsPage`     |
| a dashboard: a stat row + summary/chart cards                        | `OverviewPage`     |
| a fullbleed analytics surface (sticky filter bar + big table/charts) | `WorkbenchPage`    |
| a multi-step flow                                                    | `WizardPage`       |
| auth / standalone, outside the app shell                             | `CenteredPage`     |
| a genuine bespoke app canvas (chat, playground, builder)             | `FullBleedPage`\*  |

\* `FullBleedPage` is the **escape hatch**, not a content template. If your page
doesn't fit one of the real templates, it's this — build custom inside it and
get a design review. Don't bend `ResourceListPage` into a chat window.

All imports come from the barrel:

```tsx
import {
  ResourceListPage,
  DetailPage,
  FormPage /* … */,
} from "@/components/page-templates";
```

## ResourceListPage

```tsx
<ResourceListPage
  scope={["mcp:read", "mcp:write"]} // page-level scope gate
  title="MCP Servers"
  description="Servers exposed to your agents."
  primaryAction={<NewServerButton />} // header CTA
  search={{ value: q, onChange: setQ, placeholder: "Search servers" }}
  viewToggle={{ value: view, onChange: setView }} // grid ⇄ table
  metrics={metrics} // optional stat-header row
  isLoading={query.isPending}
  isEmpty={rows.length === 0}
  empty={{
    icon: "server",
    heading: "No servers yet",
    action: <NewServerButton />,
  }}
>
  <Table columns={columns} data={rows} rowKey={(r) => r.id} />
</ResourceListPage>
```

- `empty` is either `{ icon?, graphic?, heading, description?, action? }` (renders
  the shared `InlineEmptyState`) or a custom node.
- `filters` / `sort` are pass-throughs to `Page.Toolbar.Filters` / `.SortBy` — use
  them only if the page already uses the shared filter kit (`components/filters`).
- `loadingFallback` defaults to a shaped `<SkeletonTable/>`.

## DetailPage

Model on the **mcp / plugins** pattern: a rail (wired via the app sidebar) plus
sections. Pass `layout="routed"` (default) for one-section-at-a-time by URL path;
`"scroll"` / `"hash-scroll"` stack every section.

```tsx
<DetailPage
  breadcrumbSubstitutions={{ [slug]: entity?.name }}
  hero={<DetailHero>…</DetailHero>}
  activeSection={sectionFromPath(pathname)}
  sections={[
    {
      id: "overview",
      label: "Overview",
      href: overviewHref,
      content: <Overview />,
    },
    {
      id: "settings",
      label: "Settings",
      href: settingsHref,
      content: <Settings />,
    },
    {
      id: "danger",
      label: "Danger zone",
      href: dangerHref,
      content: <DangerZone />,
    },
  ]}
  loading={query.isPending}
  notFound={
    query.isError ? { title: "Not found", backTo: listHref } : undefined
  }
/>
```

## FormPage / SettingsPage / OverviewPage / WorkbenchPage / TabbedPage / WizardPage

```tsx
<FormPage scope="prompt:write" title="New prompt" description="…">
  <form onSubmit={onSubmit} className="flex flex-col gap-4">…</form>
</FormPage>

<SettingsPage scope="project:write" title="Project settings" description="…">
  <SettingsSection>…</SettingsSection>        {/* re-exported from the barrel */}
  <DangerSettingsSection>…</DangerSettingsSection>
</SettingsPage>

<OverviewPage title="Risk Overview" timeRange={<TimeRangePicker … />}
  metrics={metrics} metricsLoading={q.isPending}>
  <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
    <ChartCard title="Top tools" loading={q.isPending} …>…</ChartCard>
  </div>
</OverviewPage>

<WorkbenchPage scope="observe:read" tabs={<ObserveTabNav base="logs" />}>
  <ToolLogsWorkbench />
</WorkbenchPage>

<TabbedPage title="Access" activeTab={tab} tabs={[
  { value: "roles", label: "Roles", href: rolesHref },
  { value: "members", label: "Members", href: membersHref },
]}>
  {tab === "roles" ? <RolesTab /> : <MembersTab />}
</TabbedPage>

<WizardPage steps={steps} currentStepId={stepId}>
  <StepContainer icon={…} title={…} onContinue={next}>{stepBody}</StepContainer>
</WizardPage>
```

## Composite widgets (Layer 2)

These are the drop-ins the templates use; reach for them directly too instead of
hand-rolling:

- `InlineEmptyState` (`@/components/inline-empty-state`) — square dashed frame +
  square icon tile. Never hand-roll a `border-dashed` + `rounded-full` blob.
- `StatRow` (`@/components/stat-row`) — a `MetricCard` row with a loading swap.
- `ChartCard` (`@/components/chart/ChartCard`) — titled panel with loading/error,
  the shared card for overview/dashboard bodies.
- `DetailBody` (`@/components/detail-body`) — the `max-w-[1270px]` detail width.

## Rules

- One page title per page (the template renders it once).
- Cards white (`bg-card`), page gutter gray, hairline borders, square corners,
  no shadows on in-flow surfaces. Tokens only — never hardcoded Tailwind colors.
- US spelling in all user-visible copy.
- If you need a shape no template covers, that's a design-system conversation —
  don't invent a new page skeleton inline.
