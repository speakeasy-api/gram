import { InlineEmptyState } from "@/components/inline-empty-state";
import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/Button";
import { MoreActions, type Action } from "@/components/ui/MoreActions";
import { Skeleton } from "@/components/ui/Skeleton";
import { formatRelativeTime } from "@/lib/dates";
import type { Dashboard } from "@gram/client/models/components/dashboard.js";
import type { Widget } from "@gram/client/models/components/widget.js";
import { useAnalyticsDescribe } from "@gram/client/react-query/analyticsDescribe.js";
import { useDashboard } from "@gram/client/react-query/dashboard.js";
import { useEffect, useMemo, useState, type JSX } from "react";
import {
  AddWidgetDialog,
  DashboardDetailsDialog,
  DeleteDashboardDialog,
} from "./DashboardDialogs";
import {
  barValuesFromSaved,
  sameFilters,
  savedFromContext,
  savedPreset,
} from "./dashboardFilters";
import { DashboardBackLink, DashboardFrame } from "./DashboardFrame";
import { DashboardGrid } from "./DashboardGrid";
import type { GridCard } from "./dashboardLayout";
import { longestWindow } from "./exploreModel";
import { useCanEditDashboard } from "./useCanEditDashboard";
import { useCreatorName } from "./useCreatorName";
import { useDashboardMutations } from "./useDashboardMutations";
import { pageFieldsFor, usePageFilters } from "./usePageFilters";

/** What the dashboard page is told about the project around it. */
interface DashboardPageProps {
  /** The project's widgets, which the cards link to. */
  widgets: Widget[];
  /** Whether the widget list has answered; until then no card can be read. */
  widgetsLoaded: boolean;
  /** The widget list could not be fetched, so no card can be drawn. */
  widgetsFailed: boolean;
  onRetryWidgets: () => void;
  /** Where the list of dashboards is. */
  backHref: string;
  /** Open another dashboard: the copy, after duplicating. */
  onOpen: (dashboard: Dashboard) => void;
  /** This dashboard was deleted. */
  onDeleted: () => void;
}

/**
 * One dashboard, open: its name and who made it, its filter bar, then its
 * cards on the grid. Someone who may edit it adds widgets, moves cards,
 * saves the filters it opens on, and renames or deletes it here; anyone
 * else reads it, or duplicates it to get their own.
 */
export function DashboardPage({
  id,
  ...props
}: DashboardPageProps & { id: string }): JSX.Element {
  const query = useDashboard({ id });
  const back = <DashboardBackLink href={props.backHref} />;

  if (query.isPending) {
    return (
      <div className="flex flex-col gap-4" aria-busy="true">
        {back}
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }
  // A failed background refetch keeps the cached dashboard on screen.
  if (query.data === undefined) {
    return (
      <div className="flex flex-col gap-4">
        {back}
        <InlineEmptyState
          icon="triangle-alert"
          heading="This dashboard did not load"
          description="It may have been deleted, or the request did not go through."
          action={
            <Button
              variant="secondary"
              size="sm"
              onClick={() => void query.refetch()}
            >
              Try again
            </Button>
          }
        />
      </div>
    );
  }
  return <DashboardView dashboard={query.data} {...props} />;
}

function DashboardView({
  dashboard,
  widgets,
  widgetsLoaded,
  widgetsFailed,
  onRetryWidgets,
  backHref,
  onOpen,
  onDeleted,
}: DashboardPageProps & { dashboard: Dashboard }): JSX.Element {
  const creator = useCreatorName();
  const canEdit = useCanEditDashboard();
  const mutations = useDashboardMutations();
  const [renaming, setRenaming] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [adding, setAdding] = useState(false);
  const editable = canEdit(dashboard);

  // The bar offers the fields some card can be filtered by, and reads its
  // options over the datasets and the longest window the cards ask.
  const catalog = useAnalyticsDescribe().data?.datasets;
  const placed = useMemo(() => {
    const ids = new Set(dashboard.widgets.map((card) => card.widgetId));
    return widgets.filter((widget) => ids.has(widget.id));
  }, [dashboard.widgets, widgets]);
  const datasets = useMemo(
    () => [...new Set(placed.map((widget) => widget.dataset))].sort(),
    [placed],
  );
  const optionsWindow = useMemo(
    () => longestWindow(placed.map((widget) => widget.query.window)),
    [placed],
  );
  const fields = useMemo(
    () => pageFieldsFor(catalog, datasets),
    [catalog, datasets],
  );
  const bar = usePageFilters({
    fields,
    defaultPreset: savedPreset(dashboard.filters),
    optionsWindow,
    optionsDatasets: datasets,
  });
  // A dashboard opens on its saved filters, unless the link already says
  // what to show. Once per dashboard, after the bar knows its fields; until
  // then the bar is not compared with what is saved, or the defaults it
  // shows meanwhile could be offered as a save.
  const [seededFor, setSeededFor] = useState<string | null>(null);
  const { touched, apply } = bar;
  useEffect(() => {
    if (seededFor === dashboard.id || !catalog || !widgetsLoaded) return;
    if (!touched) apply(barValuesFromSaved(dashboard.filters, fields));
    setSeededFor(dashboard.id);
  }, [
    dashboard.id,
    dashboard.filters,
    fields,
    catalog,
    widgetsLoaded,
    touched,
    apply,
    seededFor,
  ]);
  // What the bar holds is the viewer's own until it is saved for everyone.
  const current = savedFromContext(bar.context, fields);
  const changed =
    seededFor === dashboard.id &&
    !sameFilters(current, dashboard.filters, fields);

  const actions: Action[] = [
    ...(editable
      ? [
          {
            label: "Rename",
            icon: "pencil" as const,
            onClick: () => setRenaming(true),
          },
        ]
      : []),
    {
      label: "Duplicate",
      icon: "copy",
      disabled: mutations.pending,
      onClick: () => mutations.duplicate(dashboard.id, onOpen),
    },
    ...(editable
      ? [
          {
            label: "Delete",
            icon: "trash" as const,
            destructive: true,
            separatorBefore: true,
            onClick: () => setDeleting(true),
          },
        ]
      : []),
  ];
  const addWidget = editable ? (
    <Button
      variant="secondary"
      size="sm"
      icon="plus"
      disabled={mutations.pending}
      onClick={() => setAdding(true)}
    >
      Add widget
    </Button>
  ) : null;

  // Each card links to a saved widget, found once the widget list answers.
  const byId = useMemo(
    () => new Map(widgets.map((widget) => [widget.id, widget])),
    [widgets],
  );
  const cards = useMemo<GridCard[]>(
    () =>
      dashboard.widgets.map((placement) => ({
        placement,
        widget: byId.get(placement.widgetId),
      })),
    [dashboard.widgets, byId],
  );
  const hasCards = dashboard.widgets.length > 0;

  let body: JSX.Element;
  if (!hasCards) {
    body = (
      <InlineEmptyState
        icon="layout-dashboard"
        heading="Nothing on this dashboard yet"
        description={
          editable
            ? "Add a saved widget to start laying it out. Each card can then be dragged and resized."
            : "Its creator has not placed any widgets on it yet."
        }
      />
    );
  } else if (widgetsFailed) {
    body = (
      <InlineEmptyState
        icon="triangle-alert"
        heading="The widgets did not load"
        description="The cards are here, but the widgets behind them could not be fetched."
        action={
          <Button variant="secondary" size="sm" onClick={onRetryWidgets}>
            Try again
          </Button>
        }
      />
    );
  } else {
    body = (
      <DashboardGrid
        cards={cards}
        page={bar.context}
        canEdit={editable}
        saving={mutations.saving}
        onSave={(placements) => mutations.saveLayout(dashboard.id, placements)}
        onRemove={(placementId) =>
          mutations.removeWidget(dashboard.id, placementId)
        }
      />
    );
  }

  return (
    <>
      <DashboardFrame
        backHref={backHref}
        name={dashboard.name}
        description={dashboard.description}
        byline={
          <>
            Created by {creator(dashboard.createdByUserId)} · Updated{" "}
            {formatRelativeTime(dashboard.updatedAt)}
            {mutations.saving ? " · Saving…" : ""}
          </>
        }
        actions={
          <>
            {addWidget}
            <MoreActions
              triggerAriaLabel={`Actions for ${dashboard.name}`}
              actions={actions}
            />
          </>
        }
        // The shared filter bar every card answers within. Picking in it
        // is the viewer's own view; Save filters makes it what the
        // dashboard opens on, for everyone.
        toolbar={
          hasCards && !widgetsFailed ? (
            <>
              <Page.Toolbar.Filters {...bar.toolbar} />
              {changed ? (
                <Page.Toolbar.Actions>
                  {editable ? (
                    <Button
                      variant="secondary"
                      size="sm"
                      icon="save"
                      disabled={mutations.pending}
                      onClick={() =>
                        mutations.saveFilters(dashboard.id, current)
                      }
                    >
                      Save filters
                    </Button>
                  ) : null}
                  <Button
                    variant="tertiary"
                    size="sm"
                    onClick={() =>
                      apply(barValuesFromSaved(dashboard.filters, fields))
                    }
                  >
                    Reset filters
                  </Button>
                </Page.Toolbar.Actions>
              ) : null}
            </>
          ) : undefined
        }
      >
        {body}
      </DashboardFrame>

      <AddWidgetDialog
        key={adding ? "adding" : "not-adding"}
        open={adding}
        widgets={widgets}
        pending={mutations.pending}
        onCancel={() => setAdding(false)}
        onAdd={(widget) =>
          mutations.addWidget(dashboard.id, widget.id, () => setAdding(false))
        }
      />
      <DashboardDetailsDialog
        key={renaming ? "renaming" : "not-renaming"}
        open={renaming}
        title="Rename dashboard"
        confirm="Rename"
        initial={{ name: dashboard.name, description: dashboard.description }}
        pending={mutations.pending}
        onCancel={() => setRenaming(false)}
        onSubmit={(details) =>
          mutations.update(dashboard.id, details, () => setRenaming(false))
        }
      />
      <DeleteDashboardDialog
        name={dashboard.name}
        open={deleting}
        pending={mutations.pending}
        onCancel={() => setDeleting(false)}
        onConfirm={() =>
          mutations.remove(dashboard.id, () => {
            setDeleting(false);
            onDeleted();
          })
        }
      />
    </>
  );
}
