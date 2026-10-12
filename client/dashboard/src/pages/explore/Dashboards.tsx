import { Page } from "@/components/page-layout";
import { WorkbenchPage } from "@/components/page-templates";
import { ReleaseStageBadge } from "@/components/release-stage-badge";
import { Skeleton } from "@/components/ui/Skeleton";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import type { Dashboard } from "@gram/client/models/components/dashboard.js";
import { useDashboards } from "@gram/client/react-query/dashboards.js";
import { useWidgets } from "@gram/client/react-query/widgets.js";
import type { JSX } from "react";
import { Outlet, useParams } from "react-router";
import { BuiltInDashboardPage } from "./BuiltInDashboardPage";
import { DashboardList } from "./DashboardList";
import { DashboardPage } from "./DashboardPage";
import { RequireExplore } from "./RequireExplore";

// Dashboards: the project's dashboards and the ones Speakeasy ships, each a
// grid of widgets under one filter bar. A card's question opens in Explore,
// where widgets are made.

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

/** The list: Speakeasy-built dashboards first, then the project's own. */
export function DashboardsIndex(): JSX.Element {
  const gramProject = useProjectSlugForRequests();
  const list = useDashboards({ gramProject });
  const open = useOpenDashboard();
  const routes = useRoutes();
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
        builtIn={list.data?.builtIn ?? []}
        isPending={list.isPending}
        isError={list.isError}
        onOpen={open}
        onOpenBuiltIn={(page) => routes.dashboards.builtIn.goTo(page.slug)}
        onRetry={() => void list.refetch()}
      />
    </>
  );
}

/** One of the project's dashboards, open. */
export function DashboardRoute(): JSX.Element {
  const { dashboardId = "" } = useParams<{ dashboardId: string }>();
  const gramProject = useProjectSlugForRequests();
  const widgets = useWidgets({ gramProject });
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

/** A dashboard Speakeasy ships, open, read only. */
export function BuiltInDashboardRoute(): JSX.Element {
  const { slug = "" } = useParams<{ slug: string }>();
  const open = useOpenDashboard();
  const routes = useRoutes();
  return (
    <BuiltInDashboardPage
      key={slug}
      slug={slug}
      backHref={routes.dashboards.href()}
      onOpen={open}
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
