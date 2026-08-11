---
"dashboard": minor
---

Introduce a paint-by-numbers page-template layer so dashboard pages share one structure. Adds `ResourceListPage`, `DetailPage`, `TabbedPage`, `FormPage`, `SettingsPage`, `OverviewPage`, `WorkbenchPage`, `WizardPage`, `CenteredPage`, and a `FullBleedPage` escape hatch (in `@/components/page-templates`), plus composite widgets `InlineEmptyState`, `StatRow`, `SummaryCard`, and `DetailBody`. Migrates ~34 pages onto the templates.

Consolidates the design-system primitives: removes the dead `Modal`/`IconButton` subsystem, folds `PrivateInput` into `Input` (new `reveal` prop), `DashboardCard` into `Card.Dashboard`, `ToggleButton` into the `SegmentedControl` module, and `Editable` into `editable-text`; renames the analytics tile `chart/MetricCard` to `StatTile` so `MetricCard` is the sole primitive; and promotes the shared detail-page primitives (`SettingsSection`, `DetailSidebarNav`) out of the mcp path into `@/components/detail`.
