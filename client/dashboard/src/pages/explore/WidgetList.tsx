import { InlineEmptyState } from "@/components/inline-empty-state";
import { Button } from "@/components/ui/Button";
import {
  MoreActions,
  type Action as MoreActionsItem,
} from "@/components/ui/MoreActions";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Skeleton } from "@/components/ui/Skeleton";
import { Table, type Column, type SortDescriptor } from "@/components/ui/Table";
import { sortTableData } from "@/components/ui/Table/sorting";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { useUser } from "@/contexts/Auth";
import { formatRelativeTime } from "@/lib/dates";
import type { Widget } from "@gram/client/models/components/widget.js";
import type { WidgetDashboard } from "@gram/client/models/components/widgetdashboard.js";
import {
  ChartArea,
  ChartBar,
  ChartColumn,
  ChartLine,
  Hash,
  LayoutGrid,
  Table as TableIcon,
  TriangleAlert,
  type LucideIcon,
} from "lucide-react";
import { useMemo, useState, type JSX } from "react";
import { useLocation, useSearchParams } from "react-router";
import { Page } from "@/components/page-layout";
import { useAnalyticsDescribe } from "@gram/client/react-query/analyticsDescribe.js";
import { WidgetCards } from "./WidgetCards";
import { findDataset, longestWindow, type ExploreSpec } from "./exploreModel";
import { pageCanFilter } from "./pageContext";
import { usePageFilters, type PageFilterField } from "./usePageFilters";
import { useCanEditWidget } from "./useCanEditWidget";
import { useCreatorName } from "./useCreatorName";
import { useWidgetMutations } from "./useWidgetMutations";
import { DeleteWidgetDialog, WidgetDetailsDialog } from "./WidgetDialogs";

// The fields the cards' filter bar may offer, in order: the dimensions most
// questions about agent activity are cut by.
const CARD_FILTER_FIELDS: readonly PageFilterField[] = [
  { field: "user", label: "User" },
  { field: "surface", label: "Agent" },
  { field: "model", label: "Model" },
  { field: "mcp_server", label: "MCP server" },
  { field: "status", label: "Status" },
];

// The sort the list opens on: the server's own order, most recently updated
// first.
const DEFAULT_SORT: SortDescriptor = { id: "updated", direction: "desc" };

const ANYONE = "anyone";
const ME = "me";
const ALL_DATASETS = "__all__";

const CHART_ICONS: Record<string, { icon: LucideIcon; label: string }> = {
  line: { icon: ChartLine, label: "Line" },
  area: { icon: ChartArea, label: "Area" },
  bar: { icon: ChartColumn, label: "Bar" },
  ranked: { icon: ChartBar, label: "Ranked" },
  table: { icon: TableIcon, label: "Table" },
  number: { icon: Hash, label: "Number" },
};

/**
 * The project's widgets, as the Widgets tab lists them: searchable by name,
 * filterable by who saved them and what they ask, and opened in the Explore
 * tab with a click. Duplicating one is how it is shared.
 */
export function WidgetList({
  widgets,
  isPending,
  isError,
  onOpen,
  onOpenQuery,
  confirmLeave,
  onDeleted,
  onExplore,
  onRetry,
}: {
  widgets: Widget[];
  isPending: boolean;
  isError: boolean;
  /** Open a widget in the Explore tab. */
  onOpen: (widget: Widget) => void;
  /** Open a question in the Explore tab: a saved widget's, or its own. */
  onOpenQuery: (spec: ExploreSpec, widgetId: string | null) => void;
  /**
   * Run this once leaving the open widget's unsaved edits is confirmed,
   * before anything is opened or copied.
   */
  confirmLeave: (proceed: () => void) => void;
  /** A widget was deleted. */
  onDeleted: (id: string) => void;
  /** Switch to the Explore tab with nothing open. */
  onExplore: () => void;
  onRetry: () => void;
}): JSX.Element {
  const user = useUser();
  const creator = useCreatorName();
  const canEdit = useCanEditWidget();
  const mutations = useWidgetMutations();

  const [view, setView] = useListView();
  const [search, setSearch] = useState("");
  const [createdBy, setCreatedBy] = useState(ANYONE);
  const [dataset, setDataset] = useState(ALL_DATASETS);
  const [sort, setSort] = useState<SortDescriptor | null>(DEFAULT_SORT);
  const [renaming, setRenaming] = useState<Widget | null>(null);
  const [deleting, setDeleting] = useState<Widget | null>(null);

  const datasets = useMemo(
    () => [...new Set(widgets.map((widget) => widget.dataset))].sort(),
    [widgets],
  );
  // The cards' filter values are read over the longest window any widget
  // asks, so a value only its oldest days hold can still be picked.
  const optionsWindow = useMemo(
    () => longestWindow(widgets.map((widget) => widget.query.window)),
    [widgets],
  );
  // The cards' filter bar offers the fields some widget can be filtered by,
  // settled on the project's widgets rather than the ones the search leaves,
  // so it does not change under someone typing. It shows in the cards view
  // only, and asks for its options only there.
  const catalog = useAnalyticsDescribe().data?.datasets;
  const cardFields = useMemo(
    () =>
      CARD_FILTER_FIELDS.filter(({ field }) =>
        datasets.some((name) =>
          pageCanFilter(findDataset(catalog ?? [], name), field),
        ),
      ),
    [catalog, datasets],
  );
  const cardFilters = usePageFilters({
    fields: cardFields,
    optionsWindow,
    optionsDatasets: datasets,
    optionsEnabled: view === "cards",
  });

  // The same actions on a row and on a card.
  const actionsFor = (widget: Widget): MoreActionsItem[] => [
    {
      label: "Open",
      icon: "square-arrow-out-up-right",
      onClick: () => confirmLeave(() => onOpen(widget)),
    },
    ...(canEdit(widget)
      ? [
          {
            label: "Rename",
            icon: "pencil" as const,
            onClick: () => setRenaming(widget),
          },
        ]
      : []),
    {
      label: "Duplicate",
      icon: "copy",
      disabled: mutations.pending,
      onClick: () => confirmLeave(() => mutations.duplicate(widget.id, onOpen)),
    },
    // Someone else's widget without project write can only be
    // copied.
    ...(canEdit(widget)
      ? [
          {
            label: "Delete",
            icon: "trash" as const,
            destructive: true,
            separatorBefore: true,
            onClick: () => setDeleting(widget),
          },
        ]
      : []),
  ];

  const columns: Column<Widget>[] = [
    {
      key: "name",
      header: "Name",
      width: "3fr",
      sortable: true,
      sortValue: (widget) => widget.name,
      render: (widget) => (
        <div className="flex min-w-0 flex-col gap-0.5">
          <span className="flex min-w-0 items-center gap-1.5">
            <span className="truncate font-medium" title={widget.name}>
              {widget.name}
            </span>
            {widget.invalidReason ? (
              <SimpleTooltip tooltip={widget.invalidReason}>
                <TriangleAlert
                  className="text-warning size-3.5 shrink-0"
                  aria-label="No longer works"
                />
              </SimpleTooltip>
            ) : null}
          </span>
          {widget.description ? (
            <span
              className="text-muted-foreground truncate text-xs"
              title={widget.description}
            >
              {widget.description}
            </span>
          ) : null}
        </div>
      ),
    },
    {
      key: "dataset",
      header: "Dataset",
      width: "1fr",
      render: (widget) => (
        <span className="font-mono text-xs">{widget.dataset}</span>
      ),
    },
    {
      key: "chart",
      header: "Chart",
      width: "1fr",
      render: (widget) => <ChartTypeCell type={widget.visualization.type} />,
    },
    {
      key: "dashboards",
      header: "Dashboards",
      width: "1fr",
      render: (widget) => <DashboardsCell dashboards={widget.dashboards} />,
    },
    {
      key: "creator",
      header: "Created by",
      width: "1.5fr",
      render: (widget) => (
        <span className="truncate">{creator(widget.createdByUserId)}</span>
      ),
    },
    {
      key: "updated",
      id: "updated",
      header: "Updated",
      width: "1fr",
      sortable: true,
      sortValue: (widget) => widget.updatedAt,
      render: (widget) => (
        <span className="text-muted-foreground">
          {formatRelativeTime(widget.updatedAt)}
        </span>
      ),
    },
    {
      key: "actions",
      header: "",
      width: "64px",
      render: (widget) => (
        // The row opens the widget on click, so the menu keeps its own.
        <span onClick={(event) => event.stopPropagation()}>
          <MoreActions
            triggerAriaLabel={`Actions for ${widget.name}`}
            actions={actionsFor(widget)}
          />
        </span>
      ),
    },
  ];

  if (isPending) {
    return (
      <div className="flex flex-col gap-2" aria-busy="true">
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-10 w-full" />
      </div>
    );
  }
  // A failed background refetch keeps the cached list on screen.
  if (isError && widgets.length === 0) {
    return (
      <InlineEmptyState
        icon="triangle-alert"
        heading="The widgets did not load"
        description="Your widgets are safe; the list could not be fetched."
        action={
          <Button variant="secondary" size="sm" onClick={onRetry}>
            Try again
          </Button>
        }
      />
    );
  }
  if (widgets.length === 0) {
    return (
      <InlineEmptyState
        icon="layout-grid"
        heading="No widgets yet"
        description="Build a question in Explore and save it as a widget to keep it here."
        action={
          <Button variant="secondary" size="sm" onClick={onExplore}>
            Go to Explore
          </Button>
        }
      />
    );
  }

  const needle = search.trim().toLowerCase();
  const filtered = widgets.filter(
    (widget) =>
      (needle === "" || widget.name.toLowerCase().includes(needle)) &&
      (createdBy === ANYONE || widget.createdByUserId === user.id) &&
      (dataset === ALL_DATASETS || widget.dataset === dataset),
  );
  const rows = sortTableData(filtered, columns, sort) as Widget[];

  return (
    <div className="flex flex-col gap-4">
      {/* The list's own controls on one row; in the cards view, the
          shared filter bar the cards answer within on the next. */}
      <Page.Toolbar>
        <Page.Toolbar.Row>
          <Page.Toolbar.Search
            value={search}
            onChange={setSearch}
            placeholder="Search widgets"
          />
          <Page.Toolbar.Leading>
            <Select value={createdBy} onValueChange={setCreatedBy}>
              <SelectTrigger className="h-10 w-44" aria-label="Created by">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ANYONE}>Created by anyone</SelectItem>
                <SelectItem value={ME}>Created by me</SelectItem>
              </SelectContent>
            </Select>
            <Select value={dataset} onValueChange={setDataset}>
              <SelectTrigger className="h-10 w-44" aria-label="Dataset">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ALL_DATASETS}>All datasets</SelectItem>
                {datasets.map((name) => (
                  <SelectItem key={name} value={name}>
                    {name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Page.Toolbar.Leading>
          <Page.Toolbar.ViewAs
            value={view === "cards" ? "grid" : "table"}
            onChange={(mode) => setView(mode === "grid" ? "cards" : "list")}
          />
        </Page.Toolbar.Row>
        {view === "cards" ? (
          <Page.Toolbar.Row>
            <Page.Toolbar.Filters {...cardFilters.toolbar} />
          </Page.Toolbar.Row>
        ) : null}
      </Page.Toolbar>
      {view === "cards" ? (
        <WidgetCards
          widgets={rows}
          page={cardFilters.context}
          actionsFor={actionsFor}
          onOpen={(spec, widgetId) =>
            confirmLeave(() => onOpenQuery(spec, widgetId ?? null))
          }
        />
      ) : (
        <Table
          columns={columns}
          data={rows}
          rowKey={(widget) => widget.id}
          onRowClick={(widget) => confirmLeave(() => onOpen(widget))}
          sort={sort}
          onSortChange={setSort}
          noResultsMessage="No widgets match these filters."
        />
      )}

      <WidgetDetailsDialog
        key={renaming?.id ?? "closed"}
        open={renaming !== null}
        title="Rename widget"
        confirm="Rename"
        initial={{
          name: renaming?.name ?? "",
          description: renaming?.description,
        }}
        pending={mutations.pending}
        onCancel={() => setRenaming(null)}
        onSubmit={(details) => {
          if (!renaming) return;
          mutations.update(
            renaming.id,
            {
              ...details,
              dataset: renaming.dataset,
              query: renaming.query,
              visualization: renaming.visualization,
            },
            () => setRenaming(null),
          );
        }}
      />
      <DeleteWidgetDialog
        name={deleting?.name ?? ""}
        dashboards={deleting?.dashboards ?? []}
        open={deleting !== null}
        pending={mutations.pending}
        onCancel={() => setDeleting(null)}
        onConfirm={() => {
          if (!deleting) return;
          const { id } = deleting;
          mutations.remove(id, () => {
            setDeleting(null);
            onDeleted(id);
          });
        }}
      />
    </div>
  );
}

type ListView = "list" | "cards";

/** The search parameter holding how the Widgets tab shows its widgets. */
const VIEW_PARAM = "view";

/**
 * Whether the Widgets tab lists widgets or draws them as cards, kept in the
 * URL beside the tab so a link opens on the same view. Switching keeps the
 * history entry's state, as switching tabs does.
 */
function useListView(): [ListView, (view: ListView) => void] {
  const [params, setParams] = useSearchParams();
  const location = useLocation();
  const view: ListView = params.get(VIEW_PARAM) === "cards" ? "cards" : "list";
  const set = (next: ListView) =>
    setParams(
      (prev) => {
        const out = new URLSearchParams(prev);
        if (next === "cards") out.set(VIEW_PARAM, "cards");
        else out.delete(VIEW_PARAM);
        return out;
      },
      { replace: true, state: location.state },
    );
  return [view, set];
}

/** How many dashboards a widget is on, naming them on hover. */
function DashboardsCell({
  dashboards,
}: {
  dashboards: WidgetDashboard[];
}): JSX.Element {
  if (dashboards.length === 0) {
    return <span className="text-muted-foreground">—</span>;
  }
  return (
    <SimpleTooltip tooltip={dashboards.map((d) => d.name).join(", ")}>
      <span className="tabular-nums">{dashboards.length}</span>
    </SimpleTooltip>
  );
}

function ChartTypeCell({ type }: { type: unknown }): JSX.Element {
  const known = typeof type === "string" ? CHART_ICONS[type] : undefined;
  const Icon = known?.icon ?? LayoutGrid;
  const label = known?.label ?? (typeof type === "string" ? type : "—");
  return (
    <span className="text-muted-foreground flex items-center gap-1.5">
      <Icon className="size-3.5 shrink-0" aria-hidden />
      {label}
    </span>
  );
}
