import { Page } from "@/components/page-layout";
import { WorkbenchPage } from "@/components/page-templates";
import { ReleaseStageBadge } from "@/components/release-stage-badge";
import { Skeleton } from "@/components/ui/Skeleton";
import { useRoutes } from "@/routes";
import type { Dashboard } from "@gram/client/models/components/dashboard.js";
import { useDashboards } from "@gram/client/react-query/dashboards.js";
import { useWidgets } from "@gram/client/react-query/widgets.js";
import type { JSX } from "react";
import { Outlet, useParams } from "react-router";
import { DashboardList } from "./DashboardList";
import { DashboardPage } from "./DashboardPage";
import { RequireExplore } from "./RequireExplore";

// Dashboards: the project's dashboards, each a grid of widgets under one
// filter bar. A card's question opens in Explore, where widgets are made.

/** The frame every Dashboards page sits in, behind the Explore rollout. */
export function DashboardsRoot(): JSX.Element {
  return (
    <WorkbenchPage scope="project:read">
      <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
        <div className="flex w-full flex-col gap-6 p-8 pb-24">
          <Page.Eyebrow />
          <RequireExplore loading={<DashboardsSkeleton />}>
            <Outlet />
          </RequireExplore>
        </div>
      </div>
    </WorkbenchPage>
  );
}

/** The list of the project's dashboards. */
export function DashboardsIndex(): JSX.Element {
  const list = useDashboards();
  const open = useOpenDashboard();
  return (
    <>
      <div className="flex min-w-0 flex-col gap-1">
        <div className="flex items-center gap-2">
          <h1 className="text-display-sm font-thin">Dashboards</h1>
          <ReleaseStageBadge stage="preview" />
        </div>
        <p className="text-muted-foreground text-sm">
          Widgets laid out together under one set of filters.
        </p>
      </div>
      <DashboardList
        dashboards={list.data?.dashboards ?? []}
        isPending={list.isPending}
        isError={list.isError}
        onOpen={open}
        onRetry={() => void list.refetch()}
      />
    </>
  );
}

/** One of the project's dashboards, open. */
export function DashboardRoute(): JSX.Element {
  const { dashboardId = "" } = useParams<{ dashboardId: string }>();
  const widgets = useWidgets();
  const open = useOpenDashboard();
  const routes = useRoutes();
  return (
    <DashboardPage
      // A different dashboard is a different page: nothing of the last
      // one's state carries over.
      key={dashboardId}
      id={dashboardId}
      widgets={widgets.data?.widgets ?? []}
      widgetsLoaded={widgets.data !== undefined}
      widgetsFailed={widgets.isError && widgets.data === undefined}
      onRetryWidgets={() => void widgets.refetch()}
      backHref={routes.dashboards.href()}
      onOpen={open}
      onDeleted={() => routes.dashboards.goTo()}
    />
  );
}

/** Opens a project dashboard: the one picked, or the copy just made. */
function useOpenDashboard(): (dashboard: Dashboard) => void {
  const routes = useRoutes();
  return (dashboard) => routes.dashboards.detail.goTo(dashboard.id);
}

function DashboardsSkeleton(): JSX.Element {
  return (
    <div className="flex flex-col gap-4" aria-busy="true">
      <Skeleton className="h-8 w-64" />
      <Skeleton className="h-64 w-full" />
    </div>
  );
}
