import { useHideInsightsDock } from "@/components/insights-context";
import { useAssistantsAvailable } from "@/hooks/useAssistantsAvailability";
import { Page } from "@/components/page-layout";
import { ProjectDashboard } from "@/components/project/ProjectDashboard";
import { RequireScope } from "@/components/require-scope";
import {
  BRAND_MESH_SURFACE_CLASS,
  BrandMeshLayers,
} from "@/components/brand-mesh";
import { cn } from "@/lib/utils";
import { ChatLanding } from "@/pages/chat/Chat";
import { useProject } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { useRoutes } from "@/routes";
import { Navigate } from "react-router";
import { HomeWidgets } from "@/components/home/HomeWidgets";
import {
  HOME_WIDGET_IDS,
  type HomeWidgetId,
} from "@/components/home/home-widget-data";
import { HomeTerminal } from "@/components/home/HomeTerminal";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { Skeleton } from "@/components/ui/Skeleton";
import { useCallback, useState } from "react";

export default function Home(): JSX.Element {
  const { hasAnyScope, hasAnyScopeInProject, isLoading } = useRBAC();
  const terminalHome = useFeatureFlag(FEATURE_FLAGS.terminalHome);
  const routes = useRoutes();
  const { id: projectId } = useProject();
  const assistantsAvailable = useAssistantsAvailable();
  // Home carries its own "Ask anything" widget, so suppress the floating dock.
  useHideInsightsDock();

  // Keep MCP-only users on the MCP page instead of rendering the project
  // overview's project:read fallback.
  if (
    !isLoading &&
    !hasAnyScope(["project:read"], projectId) &&
    hasAnyScopeInProject(["mcp:read", "mcp:write"], projectId)
  ) {
    return <Navigate to={routes.mcp.href()} replace />;
  }

  // Without the assistant there is nothing to anchor the grid, so those
  // projects keep the classic overview.
  const showTerminalHome =
    terminalHome.status === "enabled" &&
    assistantsAvailable &&
    hasAnyScope(["assistant:read"], projectId);

  if (terminalHome.status === "loading") {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <Skeleton
            label="Loading the project home"
            className="h-[32rem] w-full"
          />
        </Page.Body>
      </Page>
    );
  }

  if (showTerminalHome) {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        {/* The grid fills the viewport, so drop the default tall bottom
            padding that would otherwise leave a dead band under it. */}
        <Page.Body className="lg:pb-6">
          <RequireScope
            scope="project:read"
            resourceId={projectId}
            level="page"
          >
            <TerminalHomeGrid />
          </RequireScope>
        </Page.Body>
      </Page>
    );
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope="project:read" resourceId={projectId} level="page">
          {/* Full content width so the widget lines up with the dashboard
              below (the /chat page centers it; the home page does not). The
              compact variant drops pinned/recents — history lives on /chat. */}
          {assistantsAvailable && (
            <RequireScope
              scope="assistant:read"
              projectId={projectId}
              level="section"
            >
              <div className="w-full pt-2 pb-6">
                <div
                  className={cn(
                    BRAND_MESH_SURFACE_CLASS,
                    "border-border z-10 border p-8",
                  )}
                >
                  <BrandMeshLayers />
                  <ChatLanding compact />
                </div>
              </div>
            </RequireScope>
          )}
          <ProjectDashboard />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}

const GRID_HEIGHT_CLASS =
  "lg:h-[calc(100dvh-var(--header-height)-var(--banner-offset,0px)-3.25rem)]";

/**
 * Bento grid around the terminal: on wide screens the terminal takes the
 * right seven of twelve columns and the top two rows; tiles fill the left
 * column and the bottom row. Below `lg` everything stacks, terminal first.
 *
 * Expanding a widget switches to two columns: the left one stacks every
 * widget, the expanded one taking the free height and the rest collapsed to
 * their title bars; the terminal fills the right.
 */
function TerminalHomeGrid(): JSX.Element {
  const [expanded, setExpanded] = useState<HomeWidgetId | null>(null);
  const toggle = useCallback(
    (id: HomeWidgetId) => setExpanded((cur) => (cur === id ? null : id)),
    [],
  );

  if (expanded) {
    const classNames = HOME_WIDGET_IDS.map((id) =>
      id === expanded ? "min-h-[24rem] lg:min-h-0 flex-1" : "",
    ) as [string, string, string, string, string];
    return (
      <div
        className={cn(
          "grid grid-cols-1 gap-6 pt-2 lg:grid-cols-12",
          GRID_HEIGHT_CLASS,
        )}
      >
        <HomeTerminal className="h-[32rem] lg:col-span-7 lg:col-start-6 lg:row-start-1 lg:h-auto" />
        <div className="flex min-h-0 flex-col gap-3 lg:col-span-5 lg:col-start-1 lg:row-start-1">
          <HomeWidgets
            classNames={classNames}
            expanded={expanded}
            onToggleExpand={toggle}
          />
        </div>
      </div>
    );
  }

  return (
    // On wide screens the grid fills the viewport below the header: three
    // rows (the first taller, for Needs attention), the terminal spanning the
    // top two, and every widget scrolling inside its own card instead of
    // growing the page. Twelve columns so the left tiles get 5/12 (room for
    // full problem text) while the bottom row still splits evenly into thirds.
    <div
      className={cn(
        "grid grid-cols-1 gap-6 pt-2 lg:grid-cols-12 lg:grid-rows-[minmax(0,3fr)_minmax(0,2fr)_minmax(0,2fr)]",
        GRID_HEIGHT_CLASS,
      )}
    >
      <HomeTerminal className="h-[32rem] lg:col-span-7 lg:col-start-6 lg:row-span-2 lg:row-start-1 lg:h-auto" />
      <HomeWidgets
        classNames={[
          "lg:col-span-5 lg:col-start-1 lg:row-start-1",
          "lg:col-span-5 lg:col-start-1 lg:row-start-2",
          "lg:col-span-4",
          "lg:col-span-4",
          "lg:col-span-4",
        ]}
        expanded={null}
        onToggleExpand={toggle}
      />
    </div>
  );
}
