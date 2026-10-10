import { InlineEmptyState } from "@/components/inline-empty-state";
import { Page } from "@/components/page-layout";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import type { BuiltInDashboard } from "@gram/client/models/components/builtindashboard.js";
import type { Dashboard } from "@gram/client/models/components/dashboard.js";
import type { DashboardFilters } from "@gram/client/models/components/dashboardfilters.js";
import { useAnalyticsDescribe } from "@gram/client/react-query/analyticsDescribe.js";
import { useDashboards } from "@gram/client/react-query/dashboards.js";
import { useMemo, type JSX, type ReactNode } from "react";
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
import { useDashboardMutations } from "./useDashboardMutations";
import { pageFieldsFor, usePageFilters } from "./usePageFilters";

/** A built-in saves no filters: it opens on the defaults, as a new dashboard does. */
const NO_FILTERS: DashboardFilters = { values: {} };

/** What the page is told about the page around it. */
interface BuiltInDashboardPageProps {
  /** Where the list of dashboards is, when the page sits under one. */
  backHref?: string | undefined;
  /** Buttons drawn beside Duplicate. */
  actions?: ReactNode;
  /** Open the project dashboard Duplicate makes. */
  onOpen: (dashboard: Dashboard) => void;
}

/**
 * A dashboard Speakeasy ships with the product, open: its cards on the
 * grid under the shared filter bar, read only. Its layout is code, the
 * same in every project, so there is nothing to rename, save or lay out;
 * what the bar holds is the viewer's own. Duplicate makes a project
 * dashboard from it, with a saved widget per card, to change at will.
 */
export function BuiltInDashboardPage({
  slug,
  ...props
}: BuiltInDashboardPageProps & { slug: string }): JSX.Element {
  // The built-ins come with the dashboard list, so an open one needs no
  // request of its own.
  const list = useDashboards();
  const page = list.data?.builtIn.find((candidate) => candidate.slug === slug);
  const back =
    props.backHref === undefined ? null : (
      <DashboardBackLink href={props.backHref} />
    );

  if (list.isPending) {
    return (
      <div className="flex flex-col gap-4" aria-busy="true">
        {back}
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }
  if (page === undefined) {
    return (
      <div className="flex flex-col gap-4">
        {back}
        <InlineEmptyState
          icon="triangle-alert"
          heading="This dashboard did not load"
          description="It may have been retired, or the request did not go through."
          action={
            <Button
              variant="secondary"
              size="sm"
              onClick={() => void list.refetch()}
            >
              Try again
            </Button>
          }
        />
      </div>
    );
  }
  return <BuiltInDashboardView page={page} {...props} />;
}

function BuiltInDashboardView({
  page,
  backHref,
  actions,
  onOpen,
}: BuiltInDashboardPageProps & { page: BuiltInDashboard }): JSX.Element {
  const mutations = useDashboardMutations();

  // Each card carries its widget with it; a built-in card links to no saved
  // widget, and a read-only grid never saves a layout, so the link is never
  // read.
  const cards = useMemo<GridCard[]>(
    () =>
      page.cards.map((card, index) => ({
        placement: {
          id: `${page.slug}:${index}`,
          widgetId: "",
          x: card.x,
          y: card.y,
          w: card.w,
          h: card.h,
        },
        widget: {
          name: card.name,
          dataset: card.dataset,
          query: card.query,
          visualization: card.visualization,
        },
      })),
    [page],
  );

  // The bar offers the fields some card can be filtered by, and reads its
  // options over the datasets and the longest window the cards ask, as a
  // project dashboard's does.
  const catalog = useAnalyticsDescribe().data?.datasets;
  const datasets = useMemo(
    () => [...new Set(page.cards.map((card) => card.dataset))].sort(),
    [page],
  );
  const optionsWindow = useMemo(
    () => longestWindow(page.cards.map((card) => card.query.window)),
    [page],
  );
  const fields = useMemo(
    () => pageFieldsFor(catalog, datasets),
    [catalog, datasets],
  );
  const bar = usePageFilters({
    fields,
    defaultPreset: savedPreset(NO_FILTERS),
    optionsWindow,
    optionsDatasets: datasets,
  });
  // What the bar holds is the viewer's own, and there is nothing to save it
  // to; Reset returns it to the defaults the page opens on.
  const changed = !sameFilters(
    savedFromContext(bar.context, fields),
    NO_FILTERS,
    fields,
  );

  return (
    <DashboardFrame
      backHref={backHref}
      name={page.name}
      description={page.description}
      badge={
        <Badge variant="information" size="sm">
          Speakeasy-built
        </Badge>
      }
      byline="Built by Speakeasy and read only. Duplicate it to lay it out your own way."
      actions={
        <>
          {actions}
          <Button
            variant="secondary"
            size="sm"
            icon="copy"
            disabled={mutations.pending}
            onClick={() => mutations.duplicateBuiltIn(page.slug, onOpen)}
          >
            Duplicate
          </Button>
        </>
      }
      toolbar={
        <>
          <Page.Toolbar.Filters {...bar.toolbar} />
          {changed ? (
            <Page.Toolbar.Actions>
              <Button
                variant="tertiary"
                size="sm"
                onClick={() =>
                  bar.apply(barValuesFromSaved(NO_FILTERS, fields))
                }
              >
                Reset filters
              </Button>
            </Page.Toolbar.Actions>
          ) : null}
        </>
      }
    >
      <DashboardGrid
        cards={cards}
        page={bar.context}
        canEdit={false}
        saving={false}
        onSave={() => {}}
        onRemove={() => {}}
      />
    </DashboardFrame>
  );
}
